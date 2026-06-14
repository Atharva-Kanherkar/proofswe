package redact

import (
	"encoding/base64"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

func TestRedactCategories(t *testing.T) {
	secrets := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"ASIAIOSFODNN7EXAMPLE",
		"aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		`{"private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"}`,
		"sk_live_51N7exampleSECRET1234567890",
		"sk-abcdefghijklmnopqrstuvwxyz123456",
		"sk-ant-api03-aaaaaaaaaaaaaaaaaaaa",
		"xoxb-1234567890-abcdefghi",
		"https://hooks.slack.com/services/T000/B000/abcdef",
		"ghp_abcdefghijklmnopqrstuvwxyz123456",
		"npm_abcdefghijklmnopqrstuvwxyz123456",
		"SK0123456789abcdef0123456789abcdef",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature",
		"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
		"postgres://user:pass@example.com/db",
		"mongodb+srv://user:pass@example.com/db",
		"Authorization: Bearer abcdefghijklmnop",
		"OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz123456",
		"password=hunter2",
		"user@example.com",
		"192.168.0.1",
		"+1 415-555-0100",
		"Jane Doe",
	}
	for _, secret := range secrets {
		t.Run(secret[:min(len(secret), 24)], func(t *testing.T) {
			got, report := Scrub("before " + secret + " after")
			if strings.Contains(got, secret) {
				t.Fatalf("secret leaked after scrub:\n%s", got)
			}
			if report.SpansRedacted == 0 {
				t.Fatalf("report spans = 0 for %q", secret)
			}
		})
	}
}

func TestRedactNoCategoryRegressesToZero(t *testing.T) {
	for _, r := range Rules() {
		if r.Category == "" {
			t.Fatalf("rule has empty category: %+v", r)
		}
		if len(r.TPs) == 0 {
			t.Fatalf("rule %s has no true positives", r.Category)
		}
		for _, tp := range r.TPs {
			if !r.Pattern.MatchString(tp) {
				t.Fatalf("rule %s did not match true positive %q", r.Category, tp)
			}
		}
	}
	if len(Categories()) < 8 {
		t.Fatalf("detector taxonomy too small: %v", Categories())
	}
}

func TestRedactOverRedactionBounds(t *testing.T) {
	benign := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"0123456789abcdef0123456789abcdef01234567",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB",
		"lorem ipsum dolor sit amet",
	}
	for _, s := range benign {
		t.Run(s[:min(len(s), 16)], func(t *testing.T) {
			got, report := Scrub(s)
			if got != s {
				t.Fatalf("benign value was redacted: got %q", got)
			}
			if report.SpansRedacted != 0 {
				t.Fatalf("spans = %d, want 0", report.SpansRedacted)
			}
		})
	}
}

func TestRedactPerRuleSelfValidation(t *testing.T) {
	if failures := ValidateRules(); len(failures) > 0 {
		t.Fatalf("rule validation failed: %v", failures)
	}
}

func TestRedactReportCountsAreExact(t *testing.T) {
	input := strings.Join([]string{
		"sk-abcdefghijklmnopqrstuvwxyz123456",
		"ghp_abcdefghijklmnopqrstuvwxyz123456",
		"user@example.com",
	}, "\n")
	_, report := Scrub(input)
	if report.SpansRedacted != 3 {
		t.Fatalf("spans = %d, want 3: %+v", report.SpansRedacted, report)
	}
	want := map[string]int{"secret.openai": 1, "secret.github": 1, "pii.email": 1}
	for category, count := range want {
		if report.ByCategory[category] != count {
			t.Fatalf("category %s = %d, want %d in %+v", category, report.ByCategory[category], count, report)
		}
	}
}

func TestRedactReportNeverEchoesSecret(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
	_, report := Scrub(secret)
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("report leaked secret: %s", data)
	}
}

func TestRedactDoesNotMangleStructure(t *testing.T) {
	input := `{"token":"sk-abcdefghijklmnopqrstuvwxyz123456","ok":true}`
	got, _ := Scrub(input)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("scrubbed JSON is not parseable: %v\n%s", err, got)
	}

	diff := "diff --git a/a.go b/a.go\n+password=hunter2\n context\n"
	scrubbed, _ := Scrub(diff)
	if !strings.HasPrefix(scrubbed, "diff --git") || !strings.Contains(scrubbed, "\n+") {
		t.Fatalf("scrubbed diff lost structure:\n%s", scrubbed)
	}
}

func TestRedactPromptPII(t *testing.T) {
	input := "Email Jane Doe at jane@example.com from 192.168.0.1 or +1 415-555-0100."
	got, report := Scrub(input)
	for _, leaked := range []string{"Jane Doe", "jane@example.com", "192.168.0.1", "+1 415-555-0100"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("PII leaked %q in %s", leaked, got)
		}
	}
	if report.SpansRedacted < 4 {
		t.Fatalf("spans = %d, want >=4", report.SpansRedacted)
	}
}

func TestRedactImportPure(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(".", "redact.go"), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == "io" || strings.HasPrefix(path, "os") || strings.HasPrefix(path, "net") {
			t.Fatalf("redact package imported banned dependency %q", path)
		}
	}
}

func TestScrubIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secret := "sk-" + rapid.StringMatching(`[A-Za-z0-9]{24}`).Draw(t, "secret")
		input := rapid.String().Draw(t, "prefix") + " " + secret + " " + rapid.String().Draw(t, "suffix")
		once, _ := Scrub(input)
		twice, _ := Scrub(once)
		if once != twice {
			t.Fatalf("scrub not idempotent:\nonce=%q\ntwice=%q", once, twice)
		}
		if strings.Contains(once, secret) {
			t.Fatalf("secret survived scrub: %q", once)
		}
	})
}

func TestScrubReplaysCommittedFuzzCorpus(t *testing.T) {
	corpus := []string{
		"Authorization: Bearer abcdefghijklmnop",
		"OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz123456",
		"postgres://user:pass@example.com/db",
	}
	for _, input := range corpus {
		t.Run(input[:min(len(input), 20)], func(t *testing.T) {
			got, _ := Scrub(input)
			if got == input || strings.Contains(got, input) {
				t.Fatalf("committed fuzz corpus leaked: %q", got)
			}
		})
	}
}

func TestRedactRecallFloor(t *testing.T) {
	if testing.Short() && os.Getenv("PROOFSWE_RUN_LONG_REDACT_TEST") != "1" {
		t.Skip("long redaction recall corpus skipped in short mode")
	}
	corpus := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"sk-abcdefghijklmnopqrstuvwxyz123456",
		"ghp_abcdefghijklmnopqrstuvwxyz123456",
		"user@example.com",
		"postgres://user:pass@example.com/db",
	}
	caught := 0
	for _, secret := range corpus {
		got, _ := Scrub(secret)
		if !strings.Contains(got, secret) {
			caught++
		}
	}
	if recall := float64(caught) / float64(len(corpus)); recall < 0.95 {
		t.Fatalf("recall = %.2f, want >= 0.95", recall)
	}
}

func TestRedactRecallFloorIsEnforcedNotSkipped(t *testing.T) {
	original := registry
	defer func() { registry = original }()
	registry = nil
	got, _ := Scrub("sk-abcdefghijklmnopqrstuvwxyz123456")
	if strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz123456") {
		return
	}
	t.Fatalf("recall guard did not fail after dropping registry")
}

func FuzzScrubNeverLeaksSecretSubstring(f *testing.F) {
	f.Add("sk-abcdefghijklmnopqrstuvwxyz123456", "prefix")
	f.Add("ghp_abcdefghijklmnopqrstuvwxyz123456", "prefix")
	f.Add("user@example.com", "prefix")
	f.Fuzz(func(t *testing.T, secret, ctx string) {
		if len(secret) < 6 {
			t.Skip()
		}
		input := ctx + "\npassword=" + secret + "\n"
		got, _ := Scrub(input)
		assertNoSecret(t, got, secret)
	})
}

func FuzzScrubNeverLeaksSecretAcrossEncoding(f *testing.F) {
	f.Add("sk-abcdefghijklmnopqrstuvwxyz123456")
	f.Fuzz(func(t *testing.T, secret string) {
		if len(secret) < 8 {
			t.Skip()
		}
		encoded := base64.StdEncoding.EncodeToString([]byte(secret))
		input := "password=" + secret + "\nencoded=" + encoded + "\nurl=" + strings.ReplaceAll(secret, "-", "%2D")
		got, _ := Scrub(input)
		assertNoSecret(t, got, secret)
	})
}

func FuzzScrubNeverLeaksSecretInRedactionReport(f *testing.F) {
	f.Add("sk-abcdefghijklmnopqrstuvwxyz123456", "ctx")
	f.Fuzz(func(t *testing.T, secret, ctx string) {
		if len(secret) < 6 {
			t.Skip()
		}
		_, report := Scrub(ctx + "\nsecret=" + secret)
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		assertNoSecret(t, string(data), secret)
	})
}

func FuzzScrubNoPanicOnGarbage(f *testing.F) {
	f.Add([]byte{0xff, 0xfe, 's', 'k', '-'})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Scrub(string(data))
	})
}

func FuzzScrubTerminatesUnderAdversarialInput(f *testing.F) {
	f.Add(strings.Repeat("A", 512))
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		start := time.Now()
		_, _ = Scrub(input)
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("scrub took %s", elapsed)
		}
	})
}

func assertNoSecret(t *testing.T, got, secret string) {
	t.Helper()
	if strings.Contains(got, secret) {
		t.Fatalf("secret survived: %q in %q", secret, got)
	}
	if strings.Contains(strings.ToLower(got), strings.ToLower(secret)) {
		t.Fatalf("case-folded secret survived: %q in %q", secret, got)
	}
	if strings.Contains(NormalizeForLeakCheck(got), NormalizeForLeakCheck(secret)) {
		t.Fatalf("normalized secret survived: %q in %q", secret, got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
