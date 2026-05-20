// Hand-written Protobuf encoding for measurement.proto.
// Wire-compatible with the generated code from proto/measurement.proto.
// Replace with protoc output once protoc is available in the dev environment.

package measurepb

import (
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// Measurement mirrors the proto message defined in proto/measurement.proto.
type Measurement struct {
	DeviceId        string
	CircumferenceMm float64
	TimestampUnixMs int64
}

// Marshal encodes m into Protobuf binary format.
func Marshal(m *Measurement) []byte {
	var b []byte
	if m.DeviceId != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, m.DeviceId)
	}
	if m.CircumferenceMm != 0 {
		b = protowire.AppendTag(b, 2, protowire.Fixed64Type)
		b = protowire.AppendFixed64(b, math.Float64bits(m.CircumferenceMm))
	}
	if m.TimestampUnixMs != 0 {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(m.TimestampUnixMs))
	}
	return b
}

// Unmarshal decodes a Protobuf binary payload into m.
func Unmarshal(b []byte, m *Measurement) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		switch {
		case num == 1 && typ == protowire.BytesType:
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.DeviceId = v
			b = b[n:]
		case num == 2 && typ == protowire.Fixed64Type:
			v, n := protowire.ConsumeFixed64(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.CircumferenceMm = math.Float64frombits(v)
			b = b[n:]
		case num == 3 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			m.TimestampUnixMs = int64(v)
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		}
	}
	return nil
}
