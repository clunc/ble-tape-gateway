package gateway

import (
	"context"
	"fmt"
	"log"
	"time"

	"ble-tape-gateway/internal/ble"
	"ble-tape-gateway/internal/events"
)

type sessionState string

const (
	stateIdle         sessionState = "idle"
	stateConnecting   sessionState = "connecting"
	stateStreaming    sessionState = "streaming"
	stateDisconnected sessionState = "disconnected"
	stateStopping     sessionState = "stopping"
)

// sessionFSM manages BLE session lifecycle with reconnect behavior.
type sessionFSM struct {
	client    ble.Client
	publisher events.Publisher
	logger    *log.Logger

	state            sessionState
	inactivityWindow time.Duration
	bo               backoff
}

func newSessionFSM(client ble.Client, publisher events.Publisher, logger *log.Logger) *sessionFSM {
	return &sessionFSM{
		client:    client,
		publisher: publisher,
		logger:    logger,
		state:     stateIdle,
		// 3s gives the tape firmware room to pause between notifications without
		// triggering a spurious disconnect (1s was too tight in practice).
		inactivityWindow: 3 * time.Second,
		bo:               newBackoff(2*time.Second, 20*time.Second),
	}
}

type backoff struct {
	base    time.Duration
	max     time.Duration
	current time.Duration
}

func newBackoff(base, max time.Duration) backoff {
	return backoff{base: base, max: max, current: base}
}

func (b *backoff) next() time.Duration {
	d := b.current
	b.current = min(b.current*2, b.max)
	return d
}

func (b *backoff) reset() { b.current = b.base }

func (s *sessionFSM) run(ctx context.Context) error {
	defer s.publisher.Close(ctx)
	defer s.client.Close()

	lastActivity := time.Now()

	for {
		if ctx.Err() != nil {
			s.transition(stateStopping)
			return ctx.Err()
		}

		s.transition(stateConnecting)
		measurements, errs, err := s.client.Stream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				s.transition(stateStopping)
				return ctx.Err()
			}
			// Scan timeout means the tape is not advertising yet. Back off here
			// too so other BLE gateways can use the shared adapter between tries.
			delay := s.bo.next()
			s.logger.Printf("stream start failed: %v; retrying in %s", err, delay)
			s.transition(stateDisconnected)
			if err := s.client.Close(); err != nil {
				s.logger.Printf("closing client after failed start: %v", err)
			}
			if err := waitDelay(ctx, delay); err != nil {
				s.transition(stateStopping)
				return err
			}
			continue
		}

		s.bo.reset()
		s.transition(stateStreaming)
		lastActivity = time.Now()
		inactivity := time.NewTimer(s.inactivityWindow)

		disconnectReason := "stream ended"
	streamLoop:
		for {
			select {
			case m, ok := <-measurements:
				if !ok {
					disconnectReason = "measurement channel closed"
					break streamLoop
				}
				lastActivity = time.Now()
				resetTimer(inactivity, s.inactivityWindow)
				if err := s.publisher.Publish(ctx, m); err != nil {
					s.transition(stateStopping)
					return fmt.Errorf("publish measurement: %w", err)
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err != nil {
					disconnectReason = fmt.Sprintf("stream error: %v", err)
					break streamLoop
				}
				lastActivity = time.Now()
				resetTimer(inactivity, s.inactivityWindow)
			case <-ctx.Done():
				s.transition(stateStopping)
				return ctx.Err()
			case <-inactivity.C:
				since := time.Since(lastActivity)
				if since < s.inactivityWindow {
					resetTimer(inactivity, s.inactivityWindow-since)
					continue
				}
				disconnectReason = fmt.Sprintf("no data for %s (assuming disconnect)", since.Truncate(time.Millisecond))
				break streamLoop
			}
		}
		inactivity.Stop()

		if err := s.handleDisconnect(ctx, disconnectReason); err != nil {
			return err
		}
	}
}

func (s *sessionFSM) transition(next sessionState) {
	if s.state == next {
		return
	}
	s.logger.Printf("session state: %s -> %s", s.state, next)
	s.state = next
}

func resetTimer(t *time.Timer, d time.Duration) {
	if t == nil {
		return
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func (s *sessionFSM) handleDisconnect(ctx context.Context, reason string) error {
	delay := s.bo.next()
	s.logger.Printf("connection closed (%s); reconnecting in %s", reason, delay)
	s.transition(stateDisconnected)
	if err := s.client.Close(); err != nil {
		s.logger.Printf("closing client after disconnect: %v", err)
	}
	if err := waitDelay(ctx, delay); err != nil {
		s.transition(stateStopping)
		return err
	}
	return nil
}

func waitDelay(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
