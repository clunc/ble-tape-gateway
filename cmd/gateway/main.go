package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ble-tape-gateway/internal/ble"
	"ble-tape-gateway/internal/config"
	"ble-tape-gateway/internal/events"
	"ble-tape-gateway/internal/gateway"
)

func main() {
	logger := log.New(os.Stdout, "[main] ", log.LstdFlags|log.Lmsgprefix)
	cfg := config.FromEnv()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := buildClient(cfg, logger)
	publisher := buildPublisher(cfg, logger)
	gw := gateway.New(client, publisher, logger)

	if err := gw.Run(ctx); err != nil && err != context.Canceled {
		logger.Fatalf("gateway stopped: %v", err)
	}
	logger.Println("gateway exited")
}

func buildClient(cfg config.Config, logger *log.Logger) ble.Client {
	if cfg.Simulated {
		logger.Printf("using simulated BLE client for device %q (interval=%s)", cfg.DeviceID, cfg.SampleInterval)
		return ble.NewSimulatedClient(cfg.DeviceID, cfg.SampleInterval)
	}

	if cfg.DeviceMAC != "" {
		logger.Printf("using DSPS BLE client for device %q (name=%q, mac=%s)", cfg.DeviceID, cfg.DeviceName, cfg.DeviceMAC)
	} else {
		logger.Printf("using DSPS BLE client for device %q (name=%q)", cfg.DeviceID, cfg.DeviceName)
	}
	return ble.NewDSPSClient(cfg.DeviceID, cfg.DeviceName, cfg.DeviceMAC, cfg.AcceptLiveMeasurements, logger)
}

func buildPublisher(cfg config.Config, logger *log.Logger) events.Publisher {
	switch cfg.PublishMode {
	case "", "log":
		return events.NewLogPublisher(logger)
	default:
		logger.Printf("publish mode %q not implemented, defaulting to log", cfg.PublishMode)
		return events.NewLogPublisher(logger)
	}
}
