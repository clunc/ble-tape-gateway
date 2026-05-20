package events

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"ble-tape-gateway/internal/ble"
	"ble-tape-gateway/internal/logutil"
	"ble-tape-gateway/internal/measurepb"
)

const defaultTopic = "measurements"

// RedpandaPublisher encodes measurements as Protobuf and produces them to Redpanda.
type RedpandaPublisher struct {
	client *kgo.Client
	topic  string
	logger *log.Logger
}

// NewRedpandaPublisher dials the given broker address and returns a publisher.
func NewRedpandaPublisher(brokerAddr string) (*RedpandaPublisher, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokerAddr),
		kgo.ProducerBatchMaxBytes(1<<20),
		kgo.RecordDeliveryTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}
	return &RedpandaPublisher{
		client: client,
		topic:  defaultTopic,
		logger: logutil.New("[redpanda] ", os.Stdout),
	}, nil
}

func (p *RedpandaPublisher) Publish(ctx context.Context, m ble.Measurement) error {
	pb := &measurepb.Measurement{
		DeviceId:        m.DeviceID,
		CircumferenceMm: m.CircumferenceMM,
		TimestampUnixMs: m.Timestamp.UnixMilli(),
	}
	payload := measurepb.Marshal(pb)

	rec := &kgo.Record{
		Topic: p.topic,
		Key:   []byte(m.DeviceID),
		Value: payload,
	}

	// Fire-and-forget: do not block the measurement pipeline waiting for the
	// broker ack. Delivery errors are logged by the callback.
	p.client.Produce(ctx, rec, func(r *kgo.Record, err error) {
		if err != nil && ctx.Err() == nil {
			p.logger.Printf("produce error: %v", err)
		}
	})
	return nil
}

func (p *RedpandaPublisher) Close(_ context.Context) error {
	p.client.Close()
	return nil
}
