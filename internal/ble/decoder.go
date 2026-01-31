package ble

import (
	"bytes"
	"fmt"
)

const (
	dspsServiceUUID    = "0783B03E-8535-B5A0-7140-A304D2495CB7"
	dspsNotifyCharUUID = "0783B03E-8535-B5A0-7140-A304D2495CB8"
)

// Status indicates whether the device reported a provisional/live or confirmed measurement and which unit set it used.
type Status string

const (
	StatusLiveMetric        Status = "PM"
	StatusConfirmedMetric   Status = "SM"
	StatusLiveImperial      Status = "PI"
	StatusConfirmedImperial Status = "SI"
)

// DecodedMeasurement holds parsed data from a DSPS notification payload.
type DecodedMeasurement struct {
	CircumferenceMM int
	Status          Status
	Confirmed       bool
	Unit            string // "metric" or "imperial"
}

// DecodeDSPSPacket parses a 20-byte DSPS payload from the tape measure into a DecodedMeasurement.
func DecodeDSPSPacket(payload []byte) (DecodedMeasurement, error) {
	var decoded DecodedMeasurement

	start := bytes.IndexByte(payload, '*')
	if start == -1 {
		return decoded, fmt.Errorf("start byte not found in %x", payload)
	}

	// Tolerate leading padding bytes (e.g., leading 0x00) by resuming from the first '*'.
	p := payload[start:]
	if len(p) < 19 { // need up through status bytes [17:18]
		return decoded, fmt.Errorf("payload too short: got %d bytes after start", len(p))
	}
	if len(p) > 20 { // ignore any trailing noise beyond the 20-byte frame
		p = p[:20]
	}

	digits := []byte{p[1], p[2], p[3], p[4]}
	mm := 0
	multipliers := []int{1000, 100, 10, 1}
	for i, d := range digits {
		if d < '0' || d > '9' {
			return decoded, fmt.Errorf("non-digit measurement byte at position %d: %q", i+1, d)
		}
		mm += int(d-'0') * multipliers[i]
	}

	status := Status(string(p[17:19]))
	unit := "metric"
	if status == StatusLiveImperial || status == StatusConfirmedImperial {
		unit = "imperial"
	}

	decoded = DecodedMeasurement{
		CircumferenceMM: mm,
		Status:          status,
		Confirmed:       p[17]&0x01 == 0x01, // 'S' sets the low bit
		Unit:            unit,
	}
	return decoded, nil
}
