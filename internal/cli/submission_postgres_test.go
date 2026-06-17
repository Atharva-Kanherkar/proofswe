package cli

import (
	"strings"
	"testing"
)

func TestPostgresGetSubmissionQueryQualifiesAmbiguousColumns(t *testing.T) {
	for _, want := range []string{
		"SELECT s.submission_id, s.task_id, s.status",
		"COALESCE(s.client_version, '')",
		"s.payload_sha256",
		"COALESCE(s.scorecard_json::text, '')",
		"s.error_code, s.error_message",
		"WHERE s.submission_id = $1",
	} {
		if !strings.Contains(postgresGetSubmissionQuery, want) {
			t.Fatalf("GetSubmission query missing %q:\n%s", want, postgresGetSubmissionQuery)
		}
	}

	for _, bad := range []string{
		"SELECT submission_id, task_id",
		"COALESCE(client_version, '')",
		"payload_sha256, COALESCE(scorecard_json::text, '')",
		"error_code, error_message",
		"\nWHERE submission_id = $1",
	} {
		if strings.Contains(postgresGetSubmissionQuery, bad) {
			t.Fatalf("GetSubmission query has ambiguous fragment %q:\n%s", bad, postgresGetSubmissionQuery)
		}
	}
}
