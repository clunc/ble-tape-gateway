package ble

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// SimulatedClient emits synthetic circumference measurements at a fixed cadence.
type SimulatedClient struct {
	deviceID string
	interval time.Duration
	source   *rand.Rand

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewSimulatedClient builds a client that produces sample measurements for local development.
func NewSimulatedClient(deviceID string, interval time.Duration) *SimulatedClient {
	if interval <= 0 {
		interval = time.Second
	}

	return &SimulatedClient{
		deviceID: deviceID,
		interval: interval,
		source:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (c *SimulatedClient) Stream(ctx context.Context) (<-chan Measurement, <-chan error, error) {
	c.mu.Lock()
	streamCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	measurements := make(chan Measurement)
	errs := make(chan error, 1)

	go func() {
		defer cancel()
		defer close(measurements)
		defer close(errs)

		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				measurements <- Measurement{
					DeviceID:        c.deviceID,
					CircumferenceMM: 1000 + c.source.NormFloat64()*5,
					Timestamp:       time.Now().UTC(),
				}
			case <-streamCtx.Done():
				return
			}
		}
	}()

	return measurements, errs, nil
}

func (c *SimulatedClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return nil
}
