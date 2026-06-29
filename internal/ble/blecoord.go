package ble

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Shared BLE-adapter coordination: the gateways (walkingpad, ble-tape, obp) share
// one BlueZ adapter, and discovery is adapter-global, so concurrent scans collide
// ("Operation already in progress" / "scan was stopped unexpectedly"). We serialize
// the discovery phase with a cross-process flock on a hostPath shared by all gateway
// pods — a gateway holds it only while scanning and releases once connected.
const bleLockPath = "/var/run/ble-coord/scan.lock"

// acquireScanLock blocks until it holds the shared scan lock or ctx is done.
// Degrades to a no-op release if the lock path is unavailable, so a missing shared
// mount never stops the gateway from running.
func acquireScanLock(ctx context.Context) func() {
	_ = os.MkdirAll(filepath.Dir(bleLockPath), 0o755)
	f, err := os.OpenFile(bleLockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return func() {}
		case <-time.After(time.Duration(200+rand.Intn(500)) * time.Millisecond):
		}
	}
}
