package gateway

import (
	"context"
	"errors"
	"log"

	"ble-tape-gateway/internal/ble"
	"ble-tape-gateway/internal/events"
	"ble-tape-gateway/internal/logutil"
)

// Gateway connects BLE measurements to a downstream publisher.
type Gateway struct {
	client    ble.Client
	publisher events.Publisher
	logger    *log.Logger
}

func New(client ble.Client, publisher events.Publisher, logger *log.Logger) *Gateway {
	if logger == nil {
		logger = logutil.New("[gateway] ", nil)
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

	fsm := newSessionFSM(g.client, g.publisher, g.logger)
	return fsm.run(ctx)
}
