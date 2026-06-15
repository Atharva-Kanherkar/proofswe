package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Atharva-Kanherkar/proofswe/internal/core"
	"github.com/Atharva-Kanherkar/proofswe/internal/hashing"
	"github.com/Atharva-Kanherkar/proofswe/internal/redact"
)

// Regression for finding B: Claude tool_result blocks (user records) must be
// captured as tool OUTPUTS, not dropped. Previously toolOutputs was always empty
// for claudecode.
func TestExtractClaudeToolResultsBecomeToolOutputs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	transcript := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"please run the build"}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"running"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go build"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"build succeeded in 2s"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	out := extractTaskTranscript("claudecode", path, []byte("salt"))
	if len(out.prompts) != 1 {
		t.Fatalf("prompts = %d, want 1 (the tool_result must NOT count as a prompt)", len(out.prompts))
	}
	if len(out.toolCalls) != 1 {
		t.Fatalf("toolCalls = %d, want 1", len(out.toolCalls))
	}
	if len(out.toolOutputs) != 1 {
		t.Fatalf("toolOutputs = %d, want 1 (the bug: Claude tool results were dropped)", len(out.toolOutputs))
	}
	if !strings.Contains(out.toolOutputs[0].text, "build succeeded") {
		t.Fatalf("tool output text = %q, want the result content", out.toolOutputs[0].text)
	}
}

// Regression for finding C: at a content tier, an UNTRACKED new file's content
// must be captured in code.Patch — `git diff HEAD` alone omits new files, so the
// record previously held only the path. Test/solution split is by file role.
func TestBuildCodeRecordCapturesUntrackedAndSplitsByRole(t *testing.T) {
	h := hashing.New([]byte("salt"))
	added := []lineRef{
		{path: "internal/new.go", text: "func Added() int { return 7 }"},
		{path: "internal/new.go", text: "// brand new untracked file"},
		{path: "internal/new_test.go", text: "func TestAdded(t *testing.T) {}"},
	}
	cats := []core.ConsentCategory{core.CategoryCodeDiffs}

	code, _ := buildCodeRecord(added, h, cats, true /*codeAllowed*/, redact.Report{})
	if len(code.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(code.Files))
	}
	if !strings.Contains(code.Patch, "+++ b/internal/new.go") || !strings.Contains(code.Patch, "+func Added() int") {
		t.Fatalf("solution patch missing new-file content:\n%q", code.Patch)
	}
	if strings.Contains(code.Patch, "new_test.go") {
		t.Fatalf("test file leaked into solution patch:\n%q", code.Patch)
	}
	if !strings.Contains(code.TestPatch, "+++ b/internal/new_test.go") || !strings.Contains(code.TestPatch, "+func TestAdded") {
		t.Fatalf("test patch missing test content:\n%q", code.TestPatch)
	}

	// Gate: with code NOT allowed (e.g. private repo), only paths, no content.
	gated, _ := buildCodeRecord(added, h, cats, false /*codeAllowed*/, redact.Report{})
	if gated.Patch != "" || gated.TestPatch != "" {
		t.Fatalf("private-repo gate failed: patch=%q testPatch=%q", gated.Patch, gated.TestPatch)
	}
	if len(gated.Files) != 2 {
		t.Fatalf("files should still be listed even when content is gated: %d", len(gated.Files))
	}
}
