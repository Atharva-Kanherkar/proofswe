//go:build proofswe_fastjson

package reader

import (
	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	gojson "github.com/goccy/go-json"
)

func decodeNormalizedEvent(data []byte) (core.NormalizedEvent, error) {
	return core.UnmarshalNormalizedEventWith(data, gojson.Unmarshal)
}
