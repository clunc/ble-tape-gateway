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
	stop     chan struct{}
	once     sync.Once
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
		stop:     make(chan struct{}),
	}
}

func (c *SimulatedClient) Stream(ctx context.Context) (<-chan Measurement, <-chan error, error) {
	measurements := make(chan Measurement)
	errs := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		defer close(measurements)
		defer close(errs)

		for {
			select {
			case <-ticker.C:
				measurements <- Measurement{
					DeviceID:        c.deviceID,
					CircumferenceMM: 1000 + c.source.NormFloat64()*5,
					Timestamp:       time.Now().UTC(),
				}
			case <-ctx.Done():
				return
			case <-c.stop:
				return
			}
		}
	}()

	return measurements, errs, nil
}

func (c *SimulatedClient) Close() error {
	c.once.Do(func() {
		close(c.stop)
	})
	return nil
}
