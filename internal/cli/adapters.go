package cli

import (
	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/claudecode"
	"github.com/Atharva-Kanherkar/proofswe/internal/adapter/codex"
	"github.com/Atharva-Kanherkar/proofswe/internal/core"
)

func defaultAdapters() []core.SourceAdapter {
	return []core.SourceAdapter{
		claudecode.New(""),
		codex.New(""),
	}
}
