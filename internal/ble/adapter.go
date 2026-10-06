package ble

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

var (
	resolvedAdapter string
	resolveOnce     sync.Once
)

// resolveAdapter picks the BlueZ adapter id to use. It prefers $BLE_ADAPTER when
// that adapter actually exists, otherwise falls back to the first adapter BlueZ
// reports — so a hci0/hci1 re-enumeration across reboots doesn't break startup.
func resolveAdapter() string {
	resolveOnce.Do(func() {
		want := os.Getenv("BLE_ADAPTER")
		adapters := listBluezAdapters()
		switch {
		case len(adapters) == 0:
			if want != "" {
				resolvedAdapter = want
			} else {
				resolvedAdapter = "hci0"
			}
		case adapterInList(adapters, want):
			resolvedAdapter = want
		default:
			resolvedAdapter = adapters[0]
		}
	})
	return resolvedAdapter
}

func listBluezAdapters() []string {
	bus, err := dbus.SystemBus()
	if err != nil {
		return nil
	}
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := bus.Object("org.bluez", "/").Call(
		"org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs); err != nil {
		return nil
	}
	var out []string
	for path, ifaces := range objs {
		if _, ok := ifaces["org.bluez.Adapter1"]; ok {
			p := string(path)
			out = append(out, p[strings.LastIndex(p, "/")+1:])
		}
	}
	sort.Strings(out)
	return out
}

func adapterInList(s []string, v string) bool {
	if v == "" {
		return false
	}
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ErrAdapterUnavailable requires a process restart: both the selected BlueZ
// object path and the TinyGo adapter are cached for the lifetime of the client.
// A USB reset can remove that object and register the controller as a new hciN.
var ErrAdapterUnavailable = errors.New("BLE adapter unavailable")

func classifyAdapterError(err error) error {
	var value dbus.Error
	var pointer *dbus.Error
	var name string
	switch {
	case errors.As(err, &value):
		name = value.Name
	case errors.As(err, &pointer):
		name = pointer.Name
	}
	switch name {
	case "org.freedesktop.DBus.Error.UnknownObject",
		"org.freedesktop.DBus.Error.UnknownMethod",
		"org.freedesktop.DBus.Error.UnknownInterface",
		"org.freedesktop.DBus.Error.ServiceUnknown",
		"org.freedesktop.DBus.Error.NameHasNoOwner":
		return fmt.Errorf("%w: %w", ErrAdapterUnavailable, err)
	default:
		return err
	}
}
