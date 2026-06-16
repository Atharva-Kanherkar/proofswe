package score

const ExtractedSignalsVersion = 1

// ExtractedSignals is the versioned transcript-only signal layer. It keeps raw
// extraction evidence separate from the scored axes so benchmark consumers can
// audit why a session received each deterministic signal.
type ExtractedSignals struct {
	Version           int              `json:"version"`
	Verification      string           `json:"verification,omitempty"`
	LandingQuality    string           `json:"landing_quality,omitempty"`
	Termination       string           `json:"termination,omitempty"`
	HumanCorrections  int              `json:"human_corrections,omitempty"`
	HumanAcceptances  int              `json:"human_acceptances,omitempty"`
	ReworkCount       int              `json:"rework_count,omitempty"`
	VerifiedAfterEdit bool             `json:"verified_after_edit,omitempty"`
	Scope             ScopeSignals     `json:"scope"`
	Evidence          []SignalEvidence `json:"evidence,omitempty"`
}

type SignalEvidence struct {
	Signal string `json:"signal"`
	Value  string `json:"value,omitempty"`
	Offset int64  `json:"offset"`
	Detail string `json:"detail,omitempty"`
}

type ScopeSignals struct {
	FilesTouched     int `json:"files_touched,omitempty"`
	TestFilesTouched int `json:"test_files_touched,omitempty"`
	EditCount        int `json:"edit_count,omitempty"`
	DiffHunks        int `json:"diff_hunks,omitempty"`
}
