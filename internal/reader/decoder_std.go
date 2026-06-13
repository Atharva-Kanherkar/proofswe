//go:build !proofswe_fastjson

package reader

import "github.com/Atharva-Kanherkar/proofswe/internal/core"

func decodeNormalizedEvent(data []byte) (core.NormalizedEvent, error) {
	return core.UnmarshalNormalizedEvent(data)
}
