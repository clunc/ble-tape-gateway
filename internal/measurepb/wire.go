package measurepb

import (
	"encoding/binary"
	"fmt"
)

// WrapConfluentWire prepends Confluent wire-format framing to a raw Protobuf payload:
//
//	[0x00] [schema_id: 4 bytes BE] [0x00 message_index] [protobuf payload]
//
// The message_index 0x00 selects the first (and only) message in the .proto.
func WrapConfluentWire(schemaID int32, payload []byte) []byte {
	buf := make([]byte, 6+len(payload))
	buf[0] = 0x00
	binary.BigEndian.PutUint32(buf[1:5], uint32(schemaID))
	buf[5] = 0x00
	copy(buf[6:], payload)
	return buf
}

// UnwrapConfluentWire strips Confluent wire framing and returns the raw
// Protobuf payload. Falls through unchanged for messages without the magic
// byte (backwards-compat with plain Protobuf).
func UnwrapConfluentWire(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty message")
	}
	if data[0] != 0x00 {
		return data, nil
	}
	if len(data) < 6 {
		return nil, fmt.Errorf("Confluent wire message too short (%d bytes)", len(data))
	}
	return data[6:], nil
}
