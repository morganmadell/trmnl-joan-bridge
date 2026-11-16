package pv3

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxMsgBody caps a PV3 message body length to guard against a desynced read
// allocating absurd amounts of memory. Joan's hello body is 436/512 bytes and
// its image ACK body is 44 bytes, so 4096 is a comfortable ceiling.
const maxMsgBody = 4096

// Wire facts for framing and classification. The 20-byte outer header carries
// the little-endian body length at bytes [12:16]; the body follows.
const (
	headerLen = 20

	// Touch: a 76-byte packet whose body marks event type 6, with coordinates
	// at body+48 / body+52 (u16 LE), flipped 180° from the displayed image.
	touchLen       = 76
	touchMarkerOff = 20
	touchMarkerVal = 6
	touchXOff      = 48
	touchYOff      = 52

	// Hello telemetry: a list of [key:u32][value:u32] LE pairs starting at body
	// offset 0x34, terminated by 0xffffffff. Parse by key, not fixed offset.
	helloKVStart       = 0x34
	helloKeyBatteryPct = 10 // value = battery charge percent (0..100)
	helloKeyRSSI       = 13 // value = |dBm| (signal is negative)
	helloKeyVoltage    = 34 // value = battery millivolts

	// Body-length bands used to classify a non-touch message.
	imageAckMaxBody = 64  // image ACK body is ~44 bytes
	helloMinBody    = 400 // hello bodies are 436/512 bytes
)

// @brief One thing Joan sends on its connection.
//
// @details Exactly one of Hello, Touch, ImageAck, or Unknown. Use a type switch that covers every
// variant.
type Message interface{ isMessage() }

// @brief Device-health reading from a hello message.
//
// @details RSSIMag is the magnitude of the signal strength. The real RSSI in dBm is negative.
type Telemetry struct {
	VoltageMv  int
	RSSIMag    int
	BatteryPct int
}

// @brief Joan's status heartbeat, sent about every 3 minutes.
//
// @details Telemetry is nil when the hello has no recognized telemetry keys.
type Hello struct{ Telemetry *Telemetry }

// @brief A single screen tap with raw panel coordinates.
type Touch struct{ X, Y int }

// @brief Joan's acknowledgement that it rendered a pushed frame.
type ImageAck struct{}

// @brief A well-formed message of an unrecognized kind.
//
// @details Len is the total length, for logs.
type Unknown struct{ Len int }

func (Hello) isMessage()    {}
func (Touch) isMessage()    {}
func (ImageAck) isMessage() {}
func (Unknown) isMessage()  {}

// @brief Read one complete length-prefixed PV3 message and classify it.
//
// @details Reading whole messages keeps the reader in sync with Joan.
//
// @param r Source of bytes. Set deadlines on the underlying connection before the call.
// @return The message, or an error for a short header, a desynced length, or a truncated body.
func ReadMessage(r io.Reader) (Message, error) {
	h := make([]byte, headerLen)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	blen := binary.LittleEndian.Uint32(h[12:16])
	if blen > maxMsgBody {
		return nil, fmt.Errorf("implausible body length %d (out of sync)", blen)
	}
	body := make([]byte, blen)
	if blen > 0 {
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
	}
	return classify(body), nil
}

// @brief Map a message body to its Message type.
//
// @details A touch is recognized by length and marker. All other messages are sorted by body-length
// band.
//
// @param body Message body without the 20-byte outer header.
// @return Touch, ImageAck, Hello, or Unknown.
func classify(body []byte) Message {
	total := headerLen + len(body)
	switch {
	case total == touchLen && isTouch(body):
		return Touch{
			X: int(binary.LittleEndian.Uint16(body[touchXOff:])),
			Y: int(binary.LittleEndian.Uint16(body[touchYOff:])),
		}
	case len(body) <= imageAckMaxBody:
		return ImageAck{}
	case len(body) >= helloMinBody:
		return Hello{Telemetry: parseTelemetry(body)}
	default:
		return Unknown{Len: total}
	}
}

// @brief Check the touch marker in a message body.
//
// @param body Message body without the outer header.
// @return True when the body is long enough and has the touch marker.
func isTouch(body []byte) bool {
	return len(body) >= touchYOff+2 &&
		binary.LittleEndian.Uint16(body[touchMarkerOff:]) == touchMarkerVal
}

// @brief Read the battery, RSSI, and charge values from a hello body.
//
// @details Walks the key-value list until the end sentinel. Does no range check: the caller that
// forwards to TRMNL does that.
//
// @param body Hello body.
// @return The readings, or nil when any of the three keys is missing.
func parseTelemetry(body []byte) *Telemetry {
	var v, r, p uint32
	var haveV, haveR, haveP bool
	for off := helloKVStart; off+8 <= len(body); off += 8 {
		key := binary.LittleEndian.Uint32(body[off:])
		if key == 0xffffffff || key > 0x200 {
			break // end-of-list sentinel, or left the key-value region
		}
		val := binary.LittleEndian.Uint32(body[off+4:])
		switch key {
		case helloKeyVoltage:
			v, haveV = val, true
		case helloKeyRSSI:
			r, haveR = val, true
		case helloKeyBatteryPct:
			p, haveP = val, true
		}
	}
	if !haveV || !haveR || !haveP {
		return nil
	}
	return &Telemetry{VoltageMv: int(v), RSSIMag: int(r), BatteryPct: int(p)}
}
