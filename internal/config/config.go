package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds runtime parameters for the gateway process.
type Config struct {
	DeviceID       string
	DeviceName     string
	DeviceMAC      string
	Simulated      bool
	SampleInterval time.Duration

	PublishMode     string
	PublishEndpoint string

	AcceptLiveMeasurements bool
}

// FromEnv builds a Config from environment variables with sensible defaults for local development.
func FromEnv() Config {
	return Config{
		DeviceID:       stringOrDefault("GATEWAY_DEVICE_ID", "tape-001"),
		DeviceName:     stringOrDefault("GATEWAY_DEVICE_NAME", "ES_TAPE"),
		DeviceMAC:      stringOrDefault("GATEWAY_DEVICE_MAC", ""),
		Simulated:      boolOrDefault("GATEWAY_SIMULATED", true),
		SampleInterval: durationOrDefault("GATEWAY_SAMPLE_INTERVAL", time.Second),

		PublishMode:     stringOrDefault("GATEWAY_PUBLISH_MODE", "log"),
		PublishEndpoint: stringOrDefault("GATEWAY_PUBLISH_ENDPOINT", ""),

		AcceptLiveMeasurements: boolOrDefault("GATEWAY_ACCEPT_LIVE_MEASUREMENTS", false),
	}
}

func stringOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func boolOrDefault(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return defaultVal
}

func durationOrDefault(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		parsed, err := time.ParseDuration(v)
		if err == nil {
			return parsed
		}
	}
	return defaultVal
}
