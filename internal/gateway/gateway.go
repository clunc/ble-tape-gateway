package gateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"ble-tape-gateway/internal/ble"
	"ble-tape-gateway/internal/events"
)

// Gateway connects BLE measurements to a downstream publisher.
type Gateway struct {
	client    ble.Client
	publisher events.Publisher
	logger    *log.Logger
}

func New(client ble.Client, publisher events.Publisher, logger *log.Logger) *Gateway {
	if logger == nil {
		logger = log.New(os.Stdout, "[gateway] ", log.LstdFlags|log.Lmsgprefix)
	}

	return &Gateway{
		client:    client,
		publisher: publisher,
		logger:    logger,
	}
}

// Run starts streaming measurements until the context is canceled or the stream ends.
func (g *Gateway) Run(ctx context.Context) error {
	if g.client == nil {
		return errors.New("ble client is required")
	}
	if g.publisher == nil {
		return errors.New("publisher is required")
	}

	measurements, errs, err := g.client.Stream(ctx)
	if err != nil {
		return fmt.Errorf("start BLE stream: %w", err)
	}
	defer g.client.Close()
	defer g.publisher.Close(ctx)

	for measurements != nil || errs != nil {
		select {
		case m, ok := <-measurements:
			if !ok {
				g.logger.Println("measurement stream closed")
				measurements = nil
				continue
			}
			if err := g.publisher.Publish(ctx, m); err != nil {
				return fmt.Errorf("publish measurement: %w", err)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				g.logger.Printf("BLE stream error: %v", err)
				return fmt.Errorf("ble stream: %w", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
