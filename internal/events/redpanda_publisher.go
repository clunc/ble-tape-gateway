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
	"ble-tape-gateway/internal/schemareg"
)

const defaultTopic = "measurements"

const measurementProtoSchema = `syntax = "proto3";

package measurement.v1;

option go_package = "ble-tape-gateway/internal/measurepb";

message Measurement {
  string device_id         = 1;
  double circumference_mm  = 2;
  int64  timestamp_unix_ms = 3;
}
`

// RedpandaPublisher encodes measurements as Protobuf and produces them to Redpanda.
type RedpandaPublisher struct {
	client   *kgo.Client
	topic    string
	schemaID int32
	logger   *log.Logger
}

// NewRedpandaPublisher dials the broker and, if schemaRegistryURL is non-empty,
// registers the Measurement schema and caches the schema ID for Confluent
// wire-format framing.
func NewRedpandaPublisher(brokerAddr, schemaRegistryURL string) (*RedpandaPublisher, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokerAddr),
		kgo.ProducerBatchMaxBytes(1<<20),
		kgo.RecordDeliveryTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}

	logger := logutil.New("[redpanda] ", os.Stdout)

	var schemaID int32
	if schemaRegistryURL != "" {
		sr := schemareg.New(schemaRegistryURL)
		id, err := sr.EnsureProtobuf("measurements-value", measurementProtoSchema)
		if err != nil {
			return nil, fmt.Errorf("schema registry: %w", err)
		}
		schemaID = id
		logger.Printf("schema registered: id=%d subject=measurements-value", schemaID)
	}

	return &RedpandaPublisher{
		client:   client,
		topic:    defaultTopic,
		schemaID: schemaID,
		logger:   logger,
	}, nil
}

func (p *RedpandaPublisher) Publish(ctx context.Context, m ble.Measurement) error {
	pb := &measurepb.Measurement{
		DeviceId:        m.DeviceID,
		CircumferenceMm: m.CircumferenceMM,
		TimestampUnixMs: m.Timestamp.UnixMilli(),
	}
	payload := measurepb.Marshal(pb)

	var value []byte
	if p.schemaID > 0 {
		value = measurepb.WrapConfluentWire(p.schemaID, payload)
	} else {
		value = payload
	}

	rec := &kgo.Record{
		Topic: p.topic,
		Key:   []byte(m.DeviceID),
		Value: value,
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
