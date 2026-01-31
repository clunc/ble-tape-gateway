package ble

import (
	"context"
	"errors"
	"time"
)

// Measurement represents a single circumference reading emitted by the tape.
type Measurement struct {
	DeviceID        string
	CircumferenceMM float64
	Timestamp       time.Time
}

// Client abstracts a BLE connection that can stream measurements.
type Client interface {
	// Stream starts reading measurements. It returns a measurement channel, an error channel, or an error if the stream
	// cannot be established.
	Stream(ctx context.Context) (<-chan Measurement, <-chan error, error)
	// Close releases any underlying resources. Implementations should be idempotent.
	Close() error
}

// UnimplementedClient is a placeholder until a real BLE client is wired up.
type UnimplementedClient struct{}

func (UnimplementedClient) Stream(context.Context) (<-chan Measurement, <-chan error, error) {
	return nil, nil, errors.New("physical BLE client not implemented yet")
}

func (UnimplementedClient) Close() error { return nil }
