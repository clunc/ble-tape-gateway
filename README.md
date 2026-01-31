# ble-tape-gateway
A slim Go-based gateway that connects directly to Bluetooth Low Energy tape-measure devices, listens for real-time circumference data, and publishes structured measurement events to downstream systems. Designed for simple deployment, reliable streaming, and easy integration into modern data pipelines or IoT environments.

## DevContainer Setup

Uses host's BlueZ daemon via D-Bus. Container matches host user credentials (UID/GID/bluetooth group) for SO_PEERCRED authentication.

**Verify Bluetooth access:**
```bash
bluetoothctl -- list && echo "✓ Bluetooth works!"
hciconfig -a
```

**Configuration:** See `.devcontainer/` directory.

## Real BLE hardware
- Reopen/rebuild the devcontainer so `--device /dev/hci0` is attached (added in `.devcontainer/devcontainer.json`) and keep `capAdd: [NET_ADMIN, NET_RAW]`.
- Inside the container, ensure the adapter is up: `hciconfig hci0 up`.
- Build and grant caps once so you can run as the `vscode` user:
  ```bash
  go build -o bin/gateway ./cmd/gateway
  sudo setcap 'cap_net_admin,cap_net_raw+eip' bin/gateway
  GATEWAY_SIMULATED=false bin/gateway
  ```
- If `setcap` is unavailable, run with sudo instead: `sudo -E GATEWAY_SIMULATED=false go run ./cmd/gateway`.

## Runtime configuration

Environment variables:
- `GATEWAY_SIMULATED` (default `true`): run without hardware using synthetic data.
- `GATEWAY_DEVICE_ID` (default `tape-001`): label used in emitted measurements.
- `GATEWAY_DEVICE_NAME` (default `ES_TAPE`): BLE advertising name used to find the Renpho tape.
- `GATEWAY_DEVICE_MAC` (optional): BLE MAC address; if set, the gateway will connect to this address directly.
- `GATEWAY_ACCEPT_LIVE_MEASUREMENTS` (default `false`): if `true`, forward provisional "P*" packets instead of only confirmed "S*" packets.
