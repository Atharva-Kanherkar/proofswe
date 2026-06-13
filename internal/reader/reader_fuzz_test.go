package reader

import (
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

func FuzzParseLine(f *testing.F) {
	f.Add([]byte(eventLine(core.EventTypeSessionStart, "seed")))
	f.Add([]byte(eventLine(core.EventTypeUserPrompt, "seed") + "\n"))
	f.Add([]byte(`{"schema_version":1,"type":"future_event","payload":{"n":1}}`))
	f.Add([]byte(`{"schema_version":1,"type":`))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, line []byte) {
		event, err := ParseLine(line)
		if err != nil {
			return
		}
		data, err := core.MarshalNormalizedEvent(event)
		if err != nil {
			t.Fatalf("MarshalNormalizedEvent() error = %v", err)
		}
		if _, err := core.UnmarshalNormalizedEvent(data); err != nil {
			t.Fatalf("UnmarshalNormalizedEvent(round trip) error = %v", err)
		}
	})
}
