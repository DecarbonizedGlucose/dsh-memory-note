package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Timestamp is the protocol time type: an RFC 3339 instant with second
// precision and a recorded UTC offset. Output always uses an explicit numeric
// offset (+00:00, never Z); input must be RFC 3339 with second precision and a
// UTC offset, where Z is accepted as +00:00.
type Timestamp struct{ time.Time }

const timestampLayout = "2006-01-02T15:04:05Z07:00"

func (t Timestamp) MarshalJSON() ([]byte, error) {
	_, offset := t.Zone()
	text := t.Format("2006-01-02T15:04:05") + formatOffset(offset)
	return json.Marshal(text)
}

func (t *Timestamp) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	if !validTimestampShape(text) {
		return Invalid("timestamp must be RFC 3339 with second precision and a UTC offset")
	}
	parsed, err := time.Parse(timestampLayout, text)
	if err != nil {
		return Invalid("timestamp must be RFC 3339 with second precision and a UTC offset")
	}
	*t = Timestamp{Time: parsed}
	return nil
}

// validTimestampShape rejects anything time.Parse would accept but the
// protocol does not: missing offset, fractional seconds, lowercase z, or a
// colon-less offset.
func validTimestampShape(text string) bool {
	if len(text) == 20 && text[19] == 'Z' {
		return true
	}
	if len(text) == 25 && (text[19] == '+' || text[19] == '-') && text[22] == ':' {
		return true
	}
	return false
}

func formatOffset(seconds int) string {
	sign := '+'
	if seconds < 0 {
		sign = '-'
		seconds = -seconds
	}
	return fmt.Sprintf("%c%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
}
