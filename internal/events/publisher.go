package events

import (
	"context"
	"log"
	"os"
	"time"

	"ble-tape-gateway/internal/ble"
)

// Publisher emits measurements to downstream systems (logs, message bus, HTTP, etc).
type Publisher interface {
	Publish(ctx context.Context, m ble.Measurement) error
	Close(ctx context.Context) error
}

// LogPublisher prints measurements to stdout with timestamps for early testing.
type LogPublisher struct {
	logger *log.Logger
}

func NewLogPublisher(logger *log.Logger) *LogPublisher {
	if logger == nil {
		logger = log.New(os.Stdout, "[publisher] ", log.LstdFlags|log.Lmsgprefix)
	}
	return &LogPublisher{logger: logger}
}

func (p *LogPublisher) Publish(ctx context.Context, m ble.Measurement) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		p.logger.Printf("device=%s circumference=%.2fmm at=%s", m.DeviceID, m.CircumferenceMM, m.Timestamp.Format(time.RFC3339Nano))
		return nil
	}
}

func (p *LogPublisher) Close(context.Context) error { return nil }
