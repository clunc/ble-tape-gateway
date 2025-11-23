# ble-tape-gateway
A slim Go-based gateway that connects directly to Bluetooth Low Energy tape-measure devices, listens for real-time circumference data, and publishes structured measurement events to downstream systems. Designed for simple deployment, reliable streaming, and easy integration into modern data pipelines or IoT environments.

## DevContainer Setup

Uses host's BlueZ daemon via D-Bus. Container matches host user credentials (UID/GID/bluetooth group) for SO_PEERCRED authentication.

**Verify Bluetooth access:**
```bash
bluetoothctl -- list && echo "✓ Bluetooth works!"
```

**Configuration:** See `.devcontainer/` directory.