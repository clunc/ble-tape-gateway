package ble

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestClassifyAdapterError(t *testing.T) {
	for _, name := range []string{
		"org.freedesktop.DBus.Error.UnknownObject",
		"org.freedesktop.DBus.Error.UnknownMethod",
		"org.freedesktop.DBus.Error.UnknownInterface",
		"org.freedesktop.DBus.Error.ServiceUnknown",
		"org.freedesktop.DBus.Error.NameHasNoOwner",
	} {
		t.Run(name, func(t *testing.T) {
			original := dbus.Error{Name: name, Body: []interface{}{"adapter removed"}}
			for _, err := range []error{original, &original, fmt.Errorf("wrapped: %w", original)} {
				classified := classifyAdapterError(err)
				if !errors.Is(classified, ErrAdapterUnavailable) {
					t.Fatalf("expected restart for %v, got %v", err, classified)
				}
				var detail dbus.Error
				var detailPtr *dbus.Error
				if !errors.As(classified, &detail) && !errors.As(classified, &detailPtr) {
					t.Fatal("original D-Bus error was lost")
				}
			}
		})
	}

	for _, err := range []error{
		nil,
		ErrScanTimeout,
		errors.New("temporary transport failure"),
		dbus.Error{Name: "org.bluez.Error.NotReady"},
		dbus.Error{Name: "org.bluez.Error.InProgress"},
		dbus.Error{Name: "org.bluez.Error.Failed"},
	} {
		if classified := classifyAdapterError(err); !reflect.DeepEqual(classified, err) {
			t.Fatalf("ordinary error should remain retryable: %v became %v", err, classified)
		}
	}
}
