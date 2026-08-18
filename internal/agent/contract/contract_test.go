package contract

// The golden files under testdata/ ARE the frozen wire contract: they
// are byte-identical copies of the core repo's
// internal/pipeline/agentcontract/testdata fixtures. If a change here
// breaks these tests, it breaks the agent protocol — update both repos
// together and bump ProtocolVersion for anything not strictly
// additive-and-optional. Fixtures are regenerated in the core repo
// (UPDATE_GOLDEN=1) and copied verbatim.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func intPtr(v int) *int       { return &v }
func strPtr(s string) *string { return &s }

func canonicalFetchInput() FetchSubmissionInput {
	return FetchSubmissionInput{
		ProtocolVersion: ProtocolVersion,
		Downloads: []DownloadSpec{
			{Bucket: "course-1", Prefix: "submissions/42/", TargetDir: "."},
			{Bucket: "course-1", Prefix: "exams/7/marking-scheme/expected/", TargetDir: "expected"},
		},
	}
}

func canonicalFetchResult() FetchSubmissionResult {
	return FetchSubmissionResult{
		AgentProtocolVersion: ProtocolVersion,
		AgentVersion:         "v0.9.0",
		Files:                12,
		Bytes:                40960,
	}
}

func canonicalRunExecInput() RunExecInput {
	return RunExecInput{
		ProtocolVersion: ProtocolVersion,
		Stage:           "test",
		ScenarioCode:    "q_sa",
		Spec: ExecSpec{
			Command:          "diff",
			Args:             []string{"--ignore-case", "q_sa.txt", "expected/q_sa.expected"},
			StdinPath:        strPtr("q_sa.in"),
			StdoutPath:       strPtr("q_sa.out"),
			TimeoutMs:        10000,
			OutputLimitBytes: 64 << 20,
			Env:              map[string]string{"LANG": "C"},
			Workdir:          ".",
		},
		StdioUpload: StdioUploadSpec{
			Bucket:    "course-1",
			KeyPrefix: "runs/55/test/q_sa",
		},
	}
}

func canonicalExecResult() ExecResult {
	return ExecResult{
		Command:             "diff",
		Args:                []string{"--ignore-case", "q_sa.txt", "expected/q_sa.expected"},
		ExitCode:            intPtr(0),
		TimedOut:            false,
		OutputLimitExceeded: false,
		OutputLimitBytes:    64 << 20,
		StdoutBytes:         512,
		StderrBytes:         0,
		StartedAt:           time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC),
		EndedAt:             time.Date(2026, 6, 10, 12, 0, 1, 500000000, time.UTC),
		DurationMs:          1500,
		StdinURI:            URIFor("course-1", "runs/55/test/q_sa/stdin"),
		StdoutURI:           URIFor("course-1", "runs/55/test/q_sa/stdout"),
		StderrURI:           URIFor("course-1", "runs/55/test/q_sa/stderr"),
	}
}

func checkGolden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	got = append(got, '\n')

	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("missing golden %s (copy from the core repo): %v", name, err)
	}
	if string(want) != string(got) {
		t.Errorf("wire encoding drifted from the frozen contract for %s — if intentional, update BOTH repos' contract copies and goldens, and consider a ProtocolVersion bump\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func TestWireEncodingFrozen(t *testing.T) {
	checkGolden(t, "fetch_submission_input.golden.json", canonicalFetchInput())
	checkGolden(t, "fetch_submission_result.golden.json", canonicalFetchResult())
	checkGolden(t, "run_exec_input.golden.json", canonicalRunExecInput())
	checkGolden(t, "exec_result.golden.json", canonicalExecResult())
}

func TestWireDecodingRoundTrips(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "run_exec_input.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var in RunExecInput
	if err := json.Unmarshal(raw, &in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(canonicalRunExecInput(), in) {
		t.Errorf("RunExecInput round-trip mismatch: %+v", in)
	}

	raw, err = os.ReadFile(filepath.Join("testdata", "exec_result.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var res ExecResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if !res.StartedAt.Equal(canonicalExecResult().StartedAt) || !reflect.DeepEqual(res.Args, canonicalExecResult().Args) || *res.ExitCode != 0 {
		t.Errorf("ExecResult round-trip mismatch: %+v", res)
	}
}
