package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"ble-tape-gateway/internal/ble"
)

type failingClient struct {
	err    error
	calls  int
	closed bool
	cancel context.CancelFunc
}

func (c *failingClient) Stream(ctx context.Context) (<-chan ble.Measurement, <-chan error, error) {
	c.calls++
	if c.calls > 1 {
		c.cancel()
		return nil, nil, ctx.Err()
	}
	return nil, nil, c.err
}

func (c *failingClient) Close() error { c.closed = true; return nil }

type closingPublisher struct{ closed bool }

func (p *closingPublisher) Publish(context.Context, ble.Measurement) error { return nil }
func (p *closingPublisher) Close(context.Context) error                    { p.closed = true; return nil }

func TestSessionAdapterRecovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		startErr  error
		wantErr   error
		wantCalls int
	}{
		{"removed adapter exits for restart", fmt.Errorf("start discovery: %w", ble.ErrAdapterUnavailable), ble.ErrAdapterUnavailable, 1},
		{"sleeping tape keeps retrying", fmt.Errorf("no advertisement: %w", ble.ErrScanTimeout), context.Canceled, 2},
		{"temporary error keeps retrying", errors.New("temporary error"), context.Canceled, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			client := &failingClient{err: tc.startErr, cancel: cancel}
			publisher := &closingPublisher{}
			fsm := newSessionFSM(client, publisher, log.New(io.Discard, "", 0))
			fsm.bo = newBackoff(0, 0)
			err := fsm.run(ctx)
			if !errors.Is(err, tc.wantErr) || client.calls != tc.wantCalls {
				t.Fatalf("got error %v after %d attempts; want %v after %d", err, client.calls, tc.wantErr, tc.wantCalls)
			}
			if !client.closed || !publisher.closed {
				t.Fatal("client and publisher must close on exit")
			}
		})
	}
}
