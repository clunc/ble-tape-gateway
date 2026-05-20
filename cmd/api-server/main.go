package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/twmb/franz-go/pkg/kgo"
	_ "modernc.org/sqlite"

	"ble-tape-gateway/internal/logutil"
	"ble-tape-gateway/internal/measurepb"
)

var bodyParts = []string{
	"Waist", "Chest", "Hips", "Thigh", "Calf", "Upper Arm", "Neck", "Wrist",
}

func main() {
	logger := logutil.New("[api] ", os.Stdout)

	broker := envOr("REDPANDA_BROKER", "redpanda:9092")
	topic := envOr("MEASUREMENTS_TOPIC", "measurements")
	dbPath := envOr("DB_PATH", "/data/measurements.db")
	listenAddr := envOr("LISTEN_ADDR", ":8080")

	db := mustOpenDB(dbPath, logger)
	defer db.Close()

	hub := newHub()
	go hub.run()

	// Shared, atomically updated body part selection.
	sel := newSelection(bodyParts[0])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go consumeLoop(ctx, broker, topic, db, hub, sel, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.serveWS)
	mux.HandleFunc("/body-part", sel.handleHTTP)
	mux.HandleFunc("/measurements/today", handleTodayMeasurements(db))
	mux.HandleFunc("/measurements", handleSaveMeasurement(db))
	mux.Handle("/", http.FileServer(http.Dir("/frontend")))

	srv := &http.Server{Addr: listenAddr, Handler: mux}
	go func() {
		logger.Printf("listening on %s", listenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
	logger.Println("api-server stopped")
}

// --- Body part selection ---

type selection struct {
	ptr unsafe.Pointer // *string, swapped atomically
}

func newSelection(initial string) *selection {
	s := &selection{}
	s.set(initial)
	return s
}

func (s *selection) get() string {
	return *(*string)(atomic.LoadPointer(&s.ptr))
}

func (s *selection) set(v string) {
	atomic.StorePointer(&s.ptr, unsafe.Pointer(&v))
}

func (s *selection) handleHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"body_part":  s.get(),
			"body_parts": bodyParts,
		})
	case http.MethodPut:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 256))
		var req struct {
			BodyPart string `json:"body_part"`
		}
		if err := json.Unmarshal(body, &req); err != nil || req.BodyPart == "" {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		s.set(req.BodyPart)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- DB ---

func mustOpenDB(path string, logger *log.Logger) *sql.DB {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		logger.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS measurements (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id        TEXT    NOT NULL,
		body_part        TEXT    NOT NULL DEFAULT '',
		circumference_mm REAL    NOT NULL,
		recorded_at      INTEGER NOT NULL
	)`)
	if err != nil {
		logger.Fatalf("create table: %v", err)
	}
	// Migrate: add body_part if the table pre-dates it.
	_, _ = db.Exec(`ALTER TABLE measurements ADD COLUMN body_part TEXT NOT NULL DEFAULT ''`)
	return db
}

func insertMeasurement(db *sql.DB, m *measurepb.Measurement, bodyPart string) error {
	_, err := db.Exec(
		`INSERT INTO measurements (device_id, body_part, circumference_mm, recorded_at) VALUES (?, ?, ?, ?)`,
		m.DeviceId, bodyPart, m.CircumferenceMm, m.TimestampUnixMs,
	)
	return err
}

func handleSaveMeasurement(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			DeviceId        string  `json:"device_id"`
			BodyPart        string  `json:"body_part"`
			CircumferenceMm float64 `json:"circumference_mm"`
			TimestampUnixMs int64   `json:"timestamp_unix_ms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m := &measurepb.Measurement{
			DeviceId:        req.DeviceId,
			CircumferenceMm: req.CircumferenceMm,
			TimestampUnixMs: req.TimestampUnixMs,
		}
		if err := insertMeasurement(db, m, req.BodyPart); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleTodayMeasurements(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Start of today in UTC as Unix milliseconds.
		now := time.Now().UTC()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()

		rows, err := db.QueryContext(r.Context(),
			`SELECT body_part, circumference_mm, recorded_at
			   FROM measurements
			  WHERE recorded_at >= ?
			  ORDER BY recorded_at ASC
			  LIMIT 100`,
			startOfDay,
		)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type entry struct {
			BodyPart        string  `json:"body_part"`
			CircumferenceMm float64 `json:"circumference_mm"`
			TimestampUnixMs int64   `json:"timestamp_unix_ms"`
		}
		result := []entry{}
		for rows.Next() {
			var e entry
			if err := rows.Scan(&e.BodyPart, &e.CircumferenceMm, &e.TimestampUnixMs); err == nil {
				result = append(result, e)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// --- Kafka consumer ---

func consumeLoop(ctx context.Context, broker, topic string, db *sql.DB, hub *hub, sel *selection, logger *log.Logger) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(broker),
		kgo.ConsumeTopics(topic),
		kgo.ConsumerGroup("api-server"),
	)
	if err != nil {
		logger.Fatalf("kafka consumer: %v", err)
	}
	defer client.Close()

	logger.Printf("consuming topic %q from %s", topic, broker)
	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				logger.Printf("fetch error: %v", e.Err)
			}
			continue
		}
		fetches.EachRecord(func(rec *kgo.Record) {
			var m measurepb.Measurement
			if err := measurepb.Unmarshal(rec.Value, &m); err != nil {
				logger.Printf("unmarshal: %v", err)
				return
			}
			bp := sel.get()
			hub.broadcast(m, bp)
		})
	}
}

// --- SSE hub ---

type hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
	reg     chan chan []byte
	unreg   chan chan []byte
	msgs    chan []byte
}

func newHub() *hub {
	return &hub{
		clients: make(map[chan []byte]struct{}),
		reg:     make(chan chan []byte, 8),
		unreg:   make(chan chan []byte, 8),
		msgs:    make(chan []byte, 64),
	}
}

func (h *hub) run() {
	for {
		select {
		case c := <-h.reg:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()
		case c := <-h.unreg:
			h.mu.Lock()
			delete(h.clients, c)
			h.mu.Unlock()
			close(c)
		case msg := <-h.msgs:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *hub) broadcast(m measurepb.Measurement, bodyPart string) {
	payload, _ := json.Marshal(map[string]any{
		"device_id":         m.DeviceId,
		"body_part":         bodyPart,
		"circumference_mm":  m.CircumferenceMm,
		"timestamp_unix_ms": m.TimestampUnixMs,
	})
	select {
	case h.msgs <- payload:
	default:
	}
}

func (h *hub) serveWS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := make(chan []byte, 16)
	h.reg <- ch
	defer func() { h.unreg <- ch }()
	flusher.Flush() // send headers immediately so EventSource.onopen fires

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write(append(append([]byte("data: "), msg...), '\n', '\n'))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
