package redact

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	ScrubberVersion  = "redact/1"
	BestEffortNotice = "automated redaction is best-effort and can miss secrets"
)

type Report struct {
	ScrubberVersion  string         `json:"scrubber_version"`
	SpansRedacted    int            `json:"spans_redacted"`
	ByCategory       map[string]int `json:"by_category,omitempty"`
	BestEffortNotice string         `json:"best_effort_notice"`
}

type Rule struct {
	Category string
	Pattern  *regexp.Regexp
	TPs      []string
	FPs      []string
}

var registry = []Rule{
	rule("secret.private_key", `(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`,
		[]string{"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"}, nil),
	rule("secret.gcp", `(?s)"private_key"\s*:\s*"-----BEGIN PRIVATE KEY-----.*?-----END PRIVATE KEY-----\\n?"`,
		[]string{`"private_key":"-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"`}, nil),
	rule("secret.aws", `\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`,
		[]string{"AKIAIOSFODNN7EXAMPLE", "ASIAIOSFODNN7EXAMPLE"}, nil),
	rule("secret.aws", `(?i)\baws_(?:secret_)?access_key(?:_id)?\s*[:=]\s*["']?[A-Za-z0-9/+]{16,}["']?`,
		[]string{"aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}, nil),
	rule("secret.stripe", `\bsk_live_[A-Za-z0-9]{16,}\b`,
		[]string{"sk_live_51N7exampleSECRET1234567890"}, nil),
	rule("secret.anthropic", `\bsk-ant-[A-Za-z0-9_-]{12,}\b`,
		[]string{"sk-ant-api03-aaaaaaaaaaaaaaaaaaaa"}, nil),
	rule("secret.openai", `\bsk-(?:proj-)?[A-Za-z0-9]{20,}\b`,
		[]string{"sk-abcdefghijklmnopqrstuvwxyz123456"}, nil),
	rule("secret.slack", `\bxox[baprs]-[A-Za-z0-9-]{10,}\b`,
		[]string{"xoxb-1234567890-abcdefghi"}, nil),
	rule("secret.slack", `https://hooks\.slack\.com/services/[A-Za-z0-9/_-]+`,
		[]string{"https://hooks.slack.com/services/T000/B000/abcdef"}, nil),
	rule("secret.github", `\bgh[pousr]_[A-Za-z0-9_]{16,}\b`,
		[]string{"ghp_abcdefghijklmnopqrstuvwxyz123456"}, nil),
	rule("secret.npm", `\bnpm_[A-Za-z0-9]{16,}\b`,
		[]string{"npm_abcdefghijklmnopqrstuvwxyz123456"}, nil),
	rule("secret.twilio", `\bSK[0-9a-fA-F]{32}\b`,
		[]string{"SK0123456789abcdef0123456789abcdef"}, nil),
	rule("secret.jwt", `\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`,
		[]string{"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature"}, nil),
	rule("secret.connection", `\b(?:postgres|postgresql|mysql|mongodb(?:\+srv)?|redis)://[^\s"'<>]+:[^\s"'<>]+@[^\s"'<>]+`,
		[]string{"postgres://user:pass@example.com/db", "mongodb+srv://user:pass@example.com/db"}, nil),
	rule("secret.bearer", `(?i)\bAuthorization:\s*Bearer\s+[A-Za-z0-9._~+/=-]{8,}`,
		[]string{"Authorization: Bearer abcdefghijklmnop"}, nil),
	rule("secret.env", `(?m)\b(?:[A-Z0-9_]*(?:PASSWORD|SECRET|TOKEN|API_KEY)|[A-Z0-9_]+_KEY)\s*=\s*[^\s#]+`,
		[]string{"OPENAI_API_KEY=sk-abcdefghijklmnopqrstuvwxyz123456"}, []string{"MONKEY=banana"}),
	rule("secret.keyword", `(?i)\b(?:password|passwd|secret|token|api[_-]?key)\s*[:=]\s*["']?[^\s"',}]+["']?`,
		[]string{"password=hunter2", "api_key=abcdef123456"}, nil),
	rule("pii.email", `\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`,
		[]string{"user@example.com"}, nil),
	rule("pii.ip", `\b(?:\d{1,3}\.){3}\d{1,3}\b`,
		[]string{"192.168.0.1"}, nil),
	rule("pii.phone", `\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]\d{3}[-.\s]\d{4}\b`,
		[]string{"+1 415-555-0100", "(415) 555-0100"}, nil),
	rule("pii.name", `\b[A-Z][a-z]{2,}\s+[A-Z][a-z]{2,}\b`,
		[]string{"Jane Doe"}, []string{"Go Build"}),
}

func rule(category, pattern string, tps, fps []string) Rule {
	return Rule{
		Category: category,
		Pattern:  regexp.MustCompile(pattern),
		TPs:      append([]string(nil), tps...),
		FPs:      append([]string(nil), fps...),
	}
}

func Rules() []Rule {
	out := make([]Rule, len(registry))
	copy(out, registry)
	return out
}

func Categories() []string {
	set := map[string]bool{}
	for _, r := range registry {
		set[r.Category] = true
	}
	out := make([]string, 0, len(set))
	for category := range set {
		out = append(out, category)
	}
	sort.Strings(out)
	return out
}

func Scrub(input string) (string, Report) {
	out := input
	report := Report{
		ScrubberVersion:  ScrubberVersion,
		ByCategory:       map[string]int{},
		BestEffortNotice: BestEffortNotice,
	}
	for _, r := range registry {
		out = r.Pattern.ReplaceAllStringFunc(out, func(match string) string {
			if alreadyRedacted(match) || allowlisted(match) {
				return match
			}
			report.SpansRedacted++
			report.ByCategory[r.Category]++
			return "[REDACTED:" + r.Category + "]"
		})
	}
	out = scrubEntropy(out, &report)
	if len(report.ByCategory) == 0 {
		report.ByCategory = nil
	}
	return out, report
}

func MergeReports(reports ...Report) Report {
	merged := Report{
		ScrubberVersion:  ScrubberVersion,
		ByCategory:       map[string]int{},
		BestEffortNotice: BestEffortNotice,
	}
	for _, report := range reports {
		merged.SpansRedacted += report.SpansRedacted
		for category, count := range report.ByCategory {
			merged.ByCategory[category] += count
		}
	}
	if len(merged.ByCategory) == 0 {
		merged.ByCategory = nil
	}
	return merged
}

func ValidateRules() []string {
	var failures []string
	for _, r := range registry {
		for _, tp := range r.TPs {
			if !r.Pattern.MatchString(tp) {
				failures = append(failures, r.Category+" did not match true positive")
			}
		}
		for _, fp := range r.FPs {
			if r.Pattern.MatchString(fp) {
				failures = append(failures, r.Category+" matched false positive")
			}
		}
	}
	return failures
}

const entropyThreshold = 3.6

var entropyToken = regexp.MustCompile(`\b[A-Za-z0-9+/]{16,}={0,2}\b`)

func scrubEntropy(input string, report *Report) string {
	return entropyToken.ReplaceAllStringFunc(input, func(match string) string {
		if alreadyRedacted(match) || allowlisted(match) || entropyScore(match) < entropyThreshold {
			return match
		}
		report.SpansRedacted++
		report.ByCategory["secret.entropy"]++
		return "[REDACTED:secret.entropy]"
	})
}

func alreadyRedacted(s string) bool {
	return strings.Contains(s, "[REDACTED:")
}

var (
	uuidRe    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	base64PNG = regexp.MustCompile(`^iVBORw0KGgo[A-Za-z0-9+/=]*$`)
)

func allowlisted(s string) bool {
	trimmed := strings.TrimSpace(s)
	return uuidRe.MatchString(trimmed) || base64PNG.MatchString(trimmed)
}

func entropyScore(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	total := 0
	for _, r := range s {
		counts[r]++
		total++
	}
	if total == 0 {
		return 0
	}
	score := 0.0
	for _, count := range counts {
		p := float64(count) / float64(total)
		score -= p * math.Log2(p)
	}
	return score
}

func NormalizeForLeakCheck(s string) string {
	lowered := strings.ToLower(s)
	var b strings.Builder
	for len(lowered) > 0 {
		r, size := utf8.DecodeRuneInString(lowered)
		if r != utf8.RuneError && !strings.ContainsRune(" \t\r\n-_./:=+'\"`", r) {
			b.WriteRune(r)
		}
		lowered = lowered[size:]
	}
	return b.String()
}
