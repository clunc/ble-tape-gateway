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

	"github.com/godbus/dbus/v5"
	"tinygo.org/x/bluetooth"

	"ble-tape-gateway/internal/logutil"
)

// adapterID is the BlueZ adapter (hci0 default; BLE_ADAPTER overrides, e.g. hci1).
func adapterID() string {
	if id := os.Getenv("BLE_ADAPTER"); id != "" {
		return id
	}
	return "hci0"
}

func adapterPath() string   { return "/org/bluez/" + adapterID() }  // /org/bluez/hciN
func devPathPrefix() string { return adapterPath() + "/dev_" }      // /org/bluez/hciN/dev_

// DSPSClient connects to the Renpho RF-BMF01 tape measure over Dialog's DSPS service.
// It subscribes to notifications and decodes them into Measurement structs.
type DSPSClient struct {
	deviceID          string
	deviceName        string
	deviceMAC         string
	acceptLiveMetrics bool
	logger            *log.Logger

	mu             sync.Mutex
	cancel         context.CancelFunc
	adapterEnabled bool
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

// ErrScanTimeout is returned when no device is found within the scan window.
// It signals "device not advertising" rather than a BLE stack failure, so
// callers should retry quickly rather than applying exponential backoff.
var ErrScanTimeout = errors.New("scan timeout")

var (
	parsedDSPSServiceUUID    = mustParseUUID(dspsServiceUUID)
	parsedDSPSNotifyCharUUID = mustParseUUID(dspsNotifyCharUUID)
)

func (c *DSPSClient) run(ctx context.Context, measurements chan<- Measurement, errs chan<- error, started chan<- error) error {
	// Derive a context we cancel on return so the notification callback always
	// sees ctx.Done() before the measurements channel is closed, preventing a
	// panic from sending on a closed channel.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sendStarted := func(err error) {
		if started != nil {
			started <- err
			close(started)
			started = nil
		}
	}

	adapter := bluetooth.DefaultAdapter
	if id := os.Getenv("BLE_ADAPTER"); id != "" {
		adapter = bluetooth.NewAdapter(id)
	}
	if !c.adapterEnabled {
		if err := adapter.Enable(); err != nil {
			sendStarted(err)
			hint := ""
			if strings.Contains(err.Error(), "operation not permitted") {
				hint = " (ensure bluetoothd is running and the process can access the adapter)"
			}
			return fmt.Errorf("enable BLE adapter%s: %w", hint, err)
		}
		c.adapterEnabled = true
	}

	// Scan using a private D-Bus connection that catches both InterfacesAdded
	// (devices BlueZ hasn't seen before) and PropertiesChanged (cached devices).
	// This means RemoveDevice is never called, BlueZ's GATT cache stays intact,
	// and ServicesResolved fires in milliseconds instead of ~1.7s after a cache wipe.
	connectAddr, err := c.dbusScan(ctx)
	if err != nil {
		sendStarted(err)
		return err
	}
	connectName := connectAddr.String()
	c.logger.Printf("connecting to %s", connectName)

	disconnected := make(chan struct{}, 1)
	adapter.SetConnectHandler(func(device bluetooth.Device, connected bool) {
		if connected {
			return
		}
		if device.Address == connectAddr {
			select {
			case disconnected <- struct{}{}:
			default:
			}
		}
	})
	defer adapter.SetConnectHandler(nil)

	// BlueZ's internal connection timeout is ~40s. After a failed connect,
	// reset the adapter to clear HCI state — this avoids accumulated state
	// causing the next attempt to block for 40s as well. We do NOT call
	// Device1.Disconnect (abort), which confuses the device firmware.
	client, err := adapter.Connect(connectAddr, bluetooth.ConnectionParams{})
	if err != nil {
		var retErr error
		if strings.Contains(err.Error(), "le-connection-abort-by-local") {
			retErr = fmt.Errorf("%w: connect: %v", ErrScanTimeout, err)
		} else {
			retErr = fmt.Errorf("connect to %s: %w", connectName, err)
		}
		sendStarted(retErr)
		return retErr
	}
	defer client.Disconnect()
	c.logger.Printf("connected to %s", connectName)

	services, err := client.DiscoverServices([]bluetooth.UUID{parsedDSPSServiceUUID})
	if err != nil {
		sendStarted(err)
		return fmt.Errorf("discover services: %w", err)
	}
	if len(services) == 0 {
		sendStarted(errors.New("service not found"))
		return fmt.Errorf("service %s not found", dspsServiceUUID)
	}

	allChars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{parsedDSPSNotifyCharUUID})
	if err != nil {
		sendStarted(err)
		return fmt.Errorf("discover characteristics: %w", err)
	}
	c.logger.Printf("discovered service %s with %d characteristic(s)", strings.ToUpper(services[0].UUID().String()), len(allChars))
	if len(allChars) == 0 {
		sendStarted(errors.New("notify characteristic not found"))
		return fmt.Errorf("notify characteristic %s not found", dspsNotifyCharUUID)
	}
	notifyChar := &allChars[0]

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

		func() {
			defer func() { recover() }() // tinygo's notify goroutine may fire after close(measurements)
			select {
			case measurements <- Measurement{
				DeviceID:        c.deviceID,
				CircumferenceMM: float64(decoded.CircumferenceMM),
				Timestamp:       time.Now().UTC(),
			}:
			case <-ctx.Done():
			}
		}()
	}

	if err := notifyChar.EnableNotifications(decode); err != nil {
		var retErr error
		// "Not Connected" means the device dropped the link between our connect and
		// subscribe calls. Treat it like a scan timeout so the FSM retries in 1s
		// rather than applying exponential backoff.
		if strings.Contains(err.Error(), "Not Connected") {
			retErr = fmt.Errorf("%w: subscribe: %v", ErrScanTimeout, err)
		} else {
			retErr = fmt.Errorf("subscribe: %w", err)
		}
		sendStarted(retErr)
		return retErr
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

// dbusScan waits for the target device to appear in BlueZ's object manager using
// a private D-Bus connection. It listens for both InterfacesAdded (devices BlueZ
// hasn't seen before) and PropertiesChanged (devices already cached in BlueZ),
// so RemoveDevice is never needed and the GATT cache stays intact across reconnects.
func (c *DSPSClient) dbusScan(ctx context.Context) (bluetooth.Address, error) {
	conn, err := dbus.SystemBusPrivate()
	if err != nil {
		return bluetooth.Address{}, fmt.Errorf("dbus private: %w", err)
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return bluetooth.Address{}, fmt.Errorf("dbus auth: %w", err)
	}
	if err := conn.Hello(); err != nil {
		return bluetooth.Address{}, fmt.Errorf("dbus hello: %w", err)
	}

	targetPath := ""
	if c.deviceMAC != "" {
		targetPath = devPathPrefix() + strings.ReplaceAll(c.deviceMAC, ":", "_")
		c.logger.Printf("scanning for device (mac=%s)", c.deviceMAC)
	} else {
		c.logger.Printf("scanning for device (name=%q)", c.deviceName)
	}

	// Register for signals before starting discovery to avoid missing events.
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	conn.AddMatchSignal(
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
	)
	conn.AddMatchSignal(
		dbus.WithMatchSender("org.bluez"),
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	)

	adapterObj := conn.Object("org.bluez", dbus.ObjectPath(adapterPath()))
	if call := adapterObj.Call("org.bluez.Adapter1.StartDiscovery", 0); call.Err != nil {
		if !strings.Contains(call.Err.Error(), "Already discovering") {
			return bluetooth.Address{}, fmt.Errorf("start discovery: %w", call.Err)
		}
	}
	defer func() {
		if call := adapterObj.Call("org.bluez.Adapter1.StopDiscovery", 0); call.Err != nil {
			if !strings.Contains(call.Err.Error(), "No discovery started") {
				c.logger.Printf("stop discovery: %v", call.Err)
			}
		}
	}()

	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case sig := <-signals:
			if sig == nil {
				continue
			}
			path, ok := c.matchScanSignal(sig, targetPath)
			if !ok {
				continue
			}
			addr, err := addrFromDevicePath(string(path))
			if err != nil {
				c.logger.Printf("parse device path %s: %v", path, err)
				continue
			}
			c.logger.Printf("found device (mac=%s)", addr.String())
			return addr, nil
		case <-timeout.C:
			return bluetooth.Address{}, fmt.Errorf("%w: device not found within 10s", ErrScanTimeout)
		case <-ctx.Done():
			return bluetooth.Address{}, ctx.Err()
		}
	}
}

// matchScanSignal returns the device path if the signal matches our target.
func (c *DSPSClient) matchScanSignal(sig *dbus.Signal, targetPath string) (dbus.ObjectPath, bool) {
	switch sig.Name {
	case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
		if len(sig.Body) < 2 {
			return "", false
		}
		path, ok := sig.Body[0].(dbus.ObjectPath)
		if !ok || !strings.HasPrefix(string(path), devPathPrefix()) {
			return "", false
		}
		if targetPath != "" {
			return path, string(path) == targetPath
		}
		// Name-based: check device properties in the signal body.
		ifaces, ok := sig.Body[1].(map[string]map[string]dbus.Variant)
		if !ok {
			return "", false
		}
		devProps := ifaces["org.bluez.Device1"]
		name, _ := devProps["Name"].Value().(string)
		if !strings.EqualFold(name, c.deviceName) {
			return "", false
		}
		uuids, _ := devProps["UUIDs"].Value().([]string)
		for _, u := range uuids {
			if strings.EqualFold(u, dspsServiceUUID) {
				return path, true
			}
		}
		return "", false

	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		if len(sig.Body) < 1 {
			return "", false
		}
		iface, _ := sig.Body[0].(string)
		if iface != "org.bluez.Device1" {
			return "", false
		}
		path := sig.Path
		if !strings.HasPrefix(string(path), devPathPrefix()) {
			return "", false
		}
		if targetPath != "" {
			return path, string(path) == targetPath
		}
		return "", false
	}
	return "", false
}

// addrFromDevicePath parses a bluetooth.Address from a BlueZ device object path
// of the form /org/bluez/hci0/dev_D0_3E_7D_7A_5A_94.
func addrFromDevicePath(path string) (bluetooth.Address, error) {
	suffix := strings.TrimPrefix(path, devPathPrefix())
	macStr := strings.ReplaceAll(suffix, "_", ":")
	mac, err := bluetooth.ParseMAC(macStr)
	if err != nil {
		return bluetooth.Address{}, fmt.Errorf("parse MAC %q: %w", macStr, err)
	}
	return bluetooth.Address{MACAddress: bluetooth.MACAddress{MAC: mac}}, nil
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

// resetAdapter power-cycles the local BLE adapter via D-Bus to flush stale HCI
// state after a failed connect. The GATT attribute files on disk are untouched,
// so ServicesResolved is still fast on the next session. adapterEnabled is
// cleared so run() calls adapter.Enable() again on the next attempt.
func (c *DSPSClient) resetAdapter() {
	c.logger.Printf("resetting BLE adapter to clear HCI state")
	conn, err := dbus.SystemBusPrivate()
	if err != nil {
		c.logger.Printf("reset adapter dbus: %v", err)
		return
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return
	}
	if err := conn.Hello(); err != nil {
		return
	}
	adapterObj := conn.Object("org.bluez", dbus.ObjectPath(adapterPath()))
	adapterObj.Call("org.freedesktop.DBus.Properties.Set", 0,
		"org.bluez.Adapter1", "Powered", dbus.MakeVariant(false))
	time.Sleep(1 * time.Second)
	adapterObj.Call("org.freedesktop.DBus.Properties.Set", 0,
		"org.bluez.Adapter1", "Powered", dbus.MakeVariant(true))
	c.adapterEnabled = false
}

// abortBluezConnect cancels an in-progress connection attempt by calling
// Device1.Disconnect on a private D-Bus connection, causing the blocked
// Device1.Connect call to return with le-connection-abort-by-local.
func abortBluezConnect(mac string, logger *log.Logger) {
	conn, err := dbus.SystemBusPrivate()
	if err != nil {
		logger.Printf("abort connect dbus: %v", err)
		return
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		return
	}
	if err := conn.Hello(); err != nil {
		return
	}
	devPath := dbus.ObjectPath(devPathPrefix() + strings.ReplaceAll(strings.ToUpper(mac), ":", "_"))
	call := conn.Object("org.bluez", devPath).Call("org.bluez.Device1.Disconnect", 0)
	if call.Err != nil && !strings.Contains(call.Err.Error(), "not connected") {
		logger.Printf("abort connect %s: %v", mac, call.Err)
	}
}

// removeBlueZDevice removes the device from BlueZ's cache. Kept as a debugging
// helper; it is no longer called in normal operation (see dbusScan).
func removeBlueZDevice(mac string, logger *log.Logger) {
	conn, err := dbus.SystemBusPrivate()
	if err != nil {
		logger.Printf("dbus private connect: %v", err)
		return
	}
	defer conn.Close()
	if err := conn.Auth(nil); err != nil {
		logger.Printf("dbus auth: %v", err)
		return
	}
	if err := conn.Hello(); err != nil {
		logger.Printf("dbus hello: %v", err)
		return
	}
	devPath := dbus.ObjectPath(devPathPrefix() + strings.ReplaceAll(strings.ToUpper(mac), ":", "_"))
	call := conn.Object("org.bluez", dbus.ObjectPath(adapterPath())).Call("org.bluez.Adapter1.RemoveDevice", 0, devPath)
	if call.Err != nil && !strings.Contains(call.Err.Error(), "Does Not Exist") {
		logger.Printf("remove cached device %s: %v", mac, call.Err)
	}
}
