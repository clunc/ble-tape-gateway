package ble

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"

	"ble-tape-gateway/internal/logutil"
)

// DSPSClient connects to the Renpho RF-BMF01 tape measure over Dialog's DSPS service.
// It subscribes to notifications and decodes them into Measurement structs.
type DSPSClient struct {
	deviceID          string
	deviceName        string
	deviceMAC         string
	acceptLiveMetrics bool
	logger            *log.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewDSPSClient builds a BLE client that listens for DSPS notifications from the tape measure.
func NewDSPSClient(deviceID, deviceName, deviceMAC string, acceptLiveMetrics bool, logger *log.Logger) *DSPSClient {
	if logger == nil {
		logger = logutil.New("[ble] ", os.Stdout)
	}
	return &DSPSClient{
		deviceID:          deviceID,
		deviceName:        deviceName,
		deviceMAC:         strings.ToUpper(deviceMAC),
		acceptLiveMetrics: acceptLiveMetrics,
		logger:            logger,
	}
}

// Stream connects to the device, subscribes to notifications, and emits decoded measurements.
func (c *DSPSClient) Stream(ctx context.Context) (<-chan Measurement, <-chan error, error) {
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return nil, nil, errors.New("stream already running")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	measurements := make(chan Measurement, 32)
	errs := make(chan error, 4)

	started := make(chan error, 1)
	go func() {
		defer close(measurements)
		defer close(errs)
		defer c.clearCancel()

		if err := c.run(streamCtx, measurements, errs, started); err != nil && streamCtx.Err() == nil {
			nonBlockingSend(errs, err)
		}
	}()

	if err := <-started; err != nil {
		return nil, nil, err
	}

	return measurements, errs, nil
}

var (
	parsedDSPSServiceUUID    = mustParseUUID(dspsServiceUUID)
	parsedDSPSNotifyCharUUID = mustParseUUID(dspsNotifyCharUUID)
)

func (c *DSPSClient) run(ctx context.Context, measurements chan<- Measurement, errs chan<- error, started chan<- error) error {
	sendStarted := func(err error) {
		if started != nil {
			started <- err
			close(started)
			started = nil
		}
	}

	adapter := bluetooth.DefaultAdapter
	if err := adapter.Enable(); err != nil {
		sendStarted(err)
		hint := ""
		if strings.Contains(err.Error(), "operation not permitted") {
			hint = " (ensure bluetoothd is running and the process can access the adapter)"
		}
		return fmt.Errorf("enable BLE adapter%s: %w", hint, err)
	}

	targetDesc := fmt.Sprintf("mac=%s", c.deviceMAC)
	if c.deviceMAC == "" {
		targetDesc = fmt.Sprintf("name=%q", c.deviceName)
	}
	c.logger.Printf("scanning for device (%s)", targetDesc)
	foundCh := make(chan bluetooth.ScanResult, 1)
	scanErr := make(chan error, 1)
	var stopScan sync.Once
	stop := func() {
		stopScan.Do(func() {
			if err := adapter.StopScan(); err != nil && !strings.Contains(err.Error(), "no scan in progress") {
				c.logger.Printf("stop scan: %v", err)
			}
		})
	}

	go func() {
		if err := adapter.Scan(func(a *bluetooth.Adapter, result bluetooth.ScanResult) {
			if ctx.Err() != nil {
				stop()
				return
			}
			if c.deviceMAC != "" {
				// Hard filter: only match explicit MAC.
				if !strings.EqualFold(result.Address.String(), c.deviceMAC) {
					return
				}
			} else {
				if !strings.EqualFold(result.LocalName(), c.deviceName) {
					return
				}
				if !result.AdvertisementPayload.HasServiceUUID(parsedDSPSServiceUUID) {
					return
				}
			}

			select {
			case foundCh <- result:
			default:
			}
			stop()
		}); err != nil && ctx.Err() == nil {
			scanErr <- err
		}
	}()

	var target bluetooth.ScanResult
	select {
	case <-ctx.Done():
		stop()
		sendStarted(ctx.Err())
		return ctx.Err()
	case err := <-scanErr:
		stop()
		sendStarted(err)
		return fmt.Errorf("scan: %w", err)
	case target = <-foundCh:
	}
	stop()

	targetName := target.LocalName()
	if targetName == "" {
		targetName = "(unknown name)"
	}
	c.logDeviceInfo(target, "found target")
	c.logger.Printf("connecting to %s (%s)", targetName, target.Address.String())

	disconnected := make(chan struct{}, 1)
	adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		if connected {
			return
		}
		if device.Address == target.Address {
			select {
			case disconnected <- struct{}{}:
			default:
			}
		}
	})
	defer adapter.SetConnectHandler(nil)

	client, err := adapter.Connect(target.Address, bluetooth.ConnectionParams{})
	if err != nil {
		sendStarted(err)
		return fmt.Errorf("connect to %s: %w", targetName, err)
	}
	defer client.Disconnect()
	c.logger.Printf("connected to %s", target.Address.String())

	services, err := client.DiscoverServices([]bluetooth.UUID{parsedDSPSServiceUUID})
	if err != nil {
		sendStarted(err)
		return fmt.Errorf("discover services: %w", err)
	}
	if len(services) == 0 {
		sendStarted(errors.New("service not found"))
		return fmt.Errorf("service %s not found", dspsServiceUUID)
	}

	allChars, err := services[0].DiscoverCharacteristics(nil)
	if err != nil {
		sendStarted(err)
		return fmt.Errorf("discover characteristics: %w", err)
	}
	charUUIDs := make([]string, 0, len(allChars))
	var notifyChar *bluetooth.DeviceCharacteristic
	for i := range allChars {
		charUUIDs = append(charUUIDs, strings.ToUpper(allChars[i].UUID().String()))
		if allChars[i].UUID() == parsedDSPSNotifyCharUUID {
			notifyChar = &allChars[i]
		}
	}
	c.logger.Printf("discovered service %s with characteristics %v", strings.ToUpper(services[0].UUID().String()), charUUIDs)
	if notifyChar == nil {
		sendStarted(errors.New("notify characteristic not found"))
		return fmt.Errorf("notify characteristic %s not found", dspsNotifyCharUUID)
	}

	decode := func(data []byte) {
		c.logger.Printf("notify payload (len=%d data=%x)", len(data), data)
		// Treat every notification as activity so the session's inactivity timer does not fire
		// while the device is continuously streaming unconfirmed measurements.
		nonBlockingSend(errs, nil)
		decoded, decodeErr := DecodeDSPSPacket(data)
		if decodeErr != nil {
			// Log and continue so we can inspect malformed notifications without tearing down the session.
			c.logger.Printf("decode error on notify (len=%d data=%x): %v", len(data), data, decodeErr)
			return
		}
		c.logger.Printf("measurement: status=%s confirmed=%t unit=%s mm=%d", decoded.Status, decoded.Confirmed, decoded.Unit, decoded.CircumferenceMM)
		if !decoded.Confirmed && !c.acceptLiveMetrics {
			return
		}
		if decoded.Unit != "metric" {
			c.logger.Printf("skipping non-metric measurement with status %s", decoded.Status)
			return
		}

		select {
		case measurements <- Measurement{
			DeviceID:        c.deviceID,
			CircumferenceMM: float64(decoded.CircumferenceMM),
			Timestamp:       time.Now().UTC(),
		}:
		case <-ctx.Done():
		}
	}

	if err := notifyChar.EnableNotifications(decode); err != nil {
		sendStarted(err)
		return fmt.Errorf("subscribe: %w", err)
	}
	sendStarted(nil)
	c.logger.Printf("subscribed for notifications on %s", dspsNotifyCharUUID)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-disconnected:
		return errors.New("ble connection lost")
	}
}

// Close cancels an active stream.
func (c *DSPSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return nil
}

func (c *DSPSClient) clearCancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel = nil
}

func nonBlockingSend(errs chan<- error, err error) {
	defer func() {
		// Channel may be closed if the stream is shutting down; ignore panics from send.
		if r := recover(); r != nil {
			// intentionally swallow
		}
	}()
	select {
	case errs <- err:
	default:
	}
}

func mustParseUUID(value string) bluetooth.UUID {
	uuid, err := bluetooth.ParseUUID(strings.ToLower(value))
	if err != nil {
		panic(fmt.Sprintf("invalid UUID %q: %v", value, err))
	}
	return uuid
}

func (c *DSPSClient) logDeviceInfo(result bluetooth.ScanResult, prefix string) {
	services := result.AdvertisementPayload.ServiceUUIDs()
	serviceStrings := make([]string, 0, len(services))
	for _, s := range services {
		serviceStrings = append(serviceStrings, strings.ToUpper(s.String()))
	}
	c.logger.Printf("%s: name=%q mac=%s rssi=%d services=%v", prefix, result.LocalName(), result.Address.String(), result.RSSI, serviceStrings)
}
