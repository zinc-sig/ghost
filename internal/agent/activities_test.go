package agent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

func TestProtocolMismatch(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, newFakeStore())

	objects := []contract.ObjectSpec{{Bucket: "b", Key: "k", TargetPaths: []string{"k"}}}
	cases := []struct {
		name     string
		activity string
		input    any
	}{
		{"FetchSubmission/above range", contract.FetchSubmissionActivity, contract.FetchSubmissionInput{ProtocolVersion: contract.ProtocolVersion + 1}},
		{"FetchSubmission/below range", contract.FetchSubmissionActivity, contract.FetchSubmissionInput{ProtocolVersion: contract.MinProtocolVersion - 1}},
		{"FetchSubmission/objects at the oldest version", contract.FetchSubmissionActivity, contract.FetchSubmissionInput{ProtocolVersion: contract.MinProtocolVersion, Objects: objects}},
		{"FetchSubmission/answer at the oldest version", contract.FetchSubmissionActivity, contract.FetchSubmissionInput{ProtocolVersion: contract.MinProtocolVersion, Answer: &contract.AnswerSpec{Stem: "q"}}},
		{"RunExec/above range", contract.RunExecActivity, contract.RunExecInput{ProtocolVersion: contract.ProtocolVersion + 1}},
		{"RunExec/below range", contract.RunExecActivity, contract.RunExecInput{ProtocolVersion: contract.MinProtocolVersion - 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := env.ExecuteActivity(tc.activity, tc.input)
			if err == nil {
				t.Fatal("expected protocol mismatch error, got nil")
			}
			var appErr *temporal.ApplicationError
			if !errors.As(err, &appErr) {
				t.Fatalf("expected *temporal.ApplicationError, got %T: %v", err, err)
			}
			if appErr.Type() != contract.ProtocolMismatchErrorType {
				t.Errorf("error type = %q, want %q", appErr.Type(), contract.ProtocolMismatchErrorType)
			}
			if !appErr.NonRetryable() {
				t.Error("protocol mismatch error must be non-retryable")
			}
		})
	}
}

func TestFetchSubmission(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["course-1"] = map[string][]byte{
		"submissions/42/main.py":     []byte("print(1)\n"),
		"submissions/42/lib/util.py": []byte("util\n"),
		"assets/expected/q1.out":     []byte("ok\n"),
	}
	env := newActivityEnv(t, cfg, store)

	input := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads: []contract.DownloadSpec{
			{Bucket: "course-1", Prefix: "submissions/42/", TargetDir: "."},
			{Bucket: "course-1", Prefix: "assets/", TargetDir: "expected"},
		},
	}
	val, err := env.ExecuteActivity(contract.FetchSubmissionActivity, input)
	if err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	var res contract.FetchSubmissionResult
	if err := val.Get(&res); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}

	want := contract.FetchSubmissionResult{
		AgentProtocolVersion: contract.ProtocolVersion,
		AgentVersion:         "test",
		Files:                3,
		Bytes:                int64(len("print(1)\n") + len("util\n") + len("ok\n")),
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}

	checks := map[string]string{
		"main.py":                  "print(1)\n",
		"lib/util.py":              "util\n",
		"expected/expected/q1.out": "ok\n",
	}
	for rel, content := range checks {
		data, err := os.ReadFile(filepath.Join(cfg.Workdir, rel))
		if err != nil {
			t.Errorf("expected file %s: %v", rel, err)
			continue
		}
		if string(data) != content {
			t.Errorf("file %s = %q, want %q", rel, data, content)
		}
	}
}

// TestProtocolRangeEchoed asserts that both activities serve every version
// from MinProtocolVersion to ProtocolVersion and that the fetch handshake
// echoes the version it was sent; an agent that echoed its own constant
// would fail core's per-run version check for a run sent at the oldest
// version.
func TestProtocolRangeEchoed(t *testing.T) {
	for _, version := range []int{contract.MinProtocolVersion, contract.ProtocolVersion} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			cfg := newTestConfig(t)
			env := newActivityEnv(t, cfg, newFakeStore())

			res, err := fetchRun(t, env, contract.FetchSubmissionInput{ProtocolVersion: version})
			if err != nil {
				t.Fatalf("FetchSubmission at v%d failed: %v", version, err)
			}
			if res.AgentProtocolVersion != version {
				t.Errorf("AgentProtocolVersion = %d, want the echoed %d", res.AgentProtocolVersion, version)
			}

			exec := execRun(t, env, contract.RunExecInput{
				ProtocolVersion: version,
				Spec:            contract.ExecSpec{Command: "/bin/true", Workdir: "."},
				StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/range"},
			})
			if exec.ExitCode == nil || *exec.ExitCode != 0 {
				t.Errorf("RunExec at v%d: ExitCode = %v (error %q), want 0", version, exec.ExitCode, exec.Error)
			}
		})
	}
}

// fetchRun executes the fetch activity and decodes its result.
func fetchRun(t *testing.T, env *testsuite.TestActivityEnvironment, in contract.FetchSubmissionInput) (contract.FetchSubmissionResult, error) {
	t.Helper()
	val, err := env.ExecuteActivity(contract.FetchSubmissionActivity, in)
	if err != nil {
		return contract.FetchSubmissionResult{}, err
	}
	var res contract.FetchSubmissionResult
	if err := val.Get(&res); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	return res, nil
}

// wantFiles asserts the content of workspace files by relative path.
func wantFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("expected file %s: %v", rel, err)
			continue
		}
		if string(data) != content {
			t.Errorf("file %s = %q, want %q", rel, data, content)
		}
	}
}

// wantMissing asserts that no path exists at each relative path.
func wantMissing(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		if _, err := os.Lstat(filepath.Join(root, rel)); err == nil {
			t.Errorf("path %s exists, want it gone", rel)
		}
	}
}

// stagingInput is the golden scenario: a student delivery at the root, a
// teacher skeleton written after it, and the answer moved into the tree.
func stagingInput() contract.FetchSubmissionInput {
	return contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "course-1", Prefix: "submissions/42/", TargetDir: "."}},
		Objects: []contract.ObjectSpec{
			{Bucket: "course-1", Key: "assets/q_java/src/main/java/app/Main.java", TargetPaths: []string{"src/main/java/app/Main.java"}},
			{Bucket: "course-1", Key: "assets/q_java/pom.xml", TargetPaths: []string{"pom.xml", "examination-assets/q_java/pom.xml"}},
		},
		Answer: &contract.AnswerSpec{Stem: "q_java", TargetPath: "src/main/java/app/Solution.java"},
	}
}

func stagingStore() *fakeStore {
	store := newFakeStore()
	store.objects["course-1"] = map[string][]byte{
		"submissions/42/q_java.java":                []byte("student solution\n"),
		"submissions/42/pom.xml":                    []byte("student pom\n"),
		"assets/q_java/src/main/java/app/Main.java": []byte("teacher main\n"),
		"assets/q_java/pom.xml":                     []byte("teacher pom\n"),
	}
	return store
}

// TestFetchSubmission_ObjectsAndAnswer asserts the staging order: the
// teacher objects overwrite the delivered file at the same path, every
// target path of an object is written, and the answer ends up at its
// target with its delivered root name gone. The result counts every
// written target as a file.
func TestFetchSubmission_ObjectsAndAnswer(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, stagingStore())

	res, err := fetchRun(t, env, stagingInput())
	if err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	want := contract.FetchSubmissionResult{
		AgentProtocolVersion: contract.ProtocolVersion,
		AgentVersion:         "test",
		Files:                5,
		Bytes:                int64(len("student solution\n") + len("student pom\n") + len("teacher main\n") + 2*len("teacher pom\n")),
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"src/main/java/app/Main.java":       "teacher main\n",
		"src/main/java/app/Solution.java":   "student solution\n",
		"pom.xml":                           "teacher pom\n",
		"examination-assets/q_java/pom.xml": "teacher pom\n",
	})
	wantMissing(t, cfg.Workdir, "q_java.java")
}

// TestFetchSubmission_RetryIsIdempotent asserts that a second attempt over
// the workspace the first one left behind produces the same result and the
// same tree: the answer is re-derived from the fresh download, not from a
// staging copy of the earlier attempt.
func TestFetchSubmission_RetryIsIdempotent(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, stagingStore())

	first, err := fetchRun(t, env, stagingInput())
	if err != nil {
		t.Fatalf("first attempt failed: %v", err)
	}
	second, err := fetchRun(t, env, stagingInput())
	if err != nil {
		t.Fatalf("second attempt failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second attempt result = %+v, want %+v", second, first)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"src/main/java/app/Solution.java": "student solution\n",
		"pom.xml":                         "teacher pom\n",
	})
	wantMissing(t, cfg.Workdir, "q_java.java")
	if entries, _ := os.ReadDir(cfg.StagingDir); len(entries) != 0 {
		t.Errorf("staging dir still holds %d entries after the attempts", len(entries))
	}
}

// TestFetchSubmission_RetryOverReplacedShapes asserts that a second
// attempt succeeds when the first one changed the type of a delivered
// path: a delivered directory replaced by a teacher file or by the answer,
// and a delivered file replaced by a directory a teacher file or the
// answer needs. The re-download meets that residue first, and a fetch that
// failed on it would turn the one retry core grants into a run failure.
func TestFetchSubmission_RetryOverReplacedShapes(t *testing.T) {
	downloads := []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}}
	cases := []struct {
		name    string
		objects map[string][]byte
		in      contract.FetchSubmissionInput
		want    map[string]string
		missing []string
	}{
		{
			"delivered directory replaced by a teacher file",
			map[string][]byte{
				"sub/pom.xml/inner.txt": []byte("delivered directory\n"),
				"asset/pom.xml":         []byte("teacher pom\n"),
			},
			contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Downloads:       downloads,
				Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/pom.xml", TargetPaths: []string{"pom.xml"}}},
			},
			map[string]string{"pom.xml": "teacher pom\n"},
			[]string{"pom.xml/inner.txt"},
		},
		{
			"delivered file replaced by a teacher directory",
			map[string][]byte{
				"sub/src":         []byte("delivered file\n"),
				"asset/Main.java": []byte("teacher main\n"),
			},
			contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Downloads:       downloads,
				Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/Main.java", TargetPaths: []string{"src/Main.java"}}},
			},
			map[string]string{"src/Main.java": "teacher main\n"},
			nil,
		},
		{
			"delivered directory replaced by the answer",
			map[string][]byte{
				"sub/q_java.java":         []byte("student\n"),
				"sub/src/Solution.java/x": []byte("delivered directory\n"),
			},
			contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Downloads:       downloads,
				Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "src/Solution.java"},
			},
			map[string]string{"src/Solution.java": "student\n"},
			[]string{"q_java.java", "src/Solution.java/x"},
		},
		{
			"delivered file replaced by the answer's parent directory",
			map[string][]byte{
				"sub/q_java.java": []byte("student\n"),
				"sub/src":         []byte("delivered file\n"),
			},
			contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Downloads:       downloads,
				Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "src/Solution.java"},
			},
			map[string]string{"src/Solution.java": "student\n"},
			[]string{"q_java.java"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			store := newFakeStore()
			store.objects["b"] = tc.objects
			env := newActivityEnv(t, cfg, store)

			first, err := fetchRun(t, env, tc.in)
			if err != nil {
				t.Fatalf("first attempt failed: %v", err)
			}
			second, err := fetchRun(t, env, tc.in)
			if err != nil {
				t.Fatalf("second attempt over the first one's residue failed: %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Errorf("second attempt result = %+v, want %+v", second, first)
			}
			wantFiles(t, cfg.Workdir, tc.want)
			wantMissing(t, cfg.Workdir, tc.missing...)
		})
	}
}

// TestFetchSubmission_AnswerRestoredAtRootName asserts that an empty
// target restores the answer under its delivered name after the objects,
// so a teacher object with the same name loses to the student's file.
func TestFetchSubmission_AnswerRestoredAtRootName(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/q_java.java": []byte("student\n"),
		"asset/stub.java": []byte("teacher stub\n"),
	}
	env := newActivityEnv(t, cfg, store)

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
		Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/stub.java", TargetPaths: []string{"q_java.java"}}},
		Answer:          &contract.AnswerSpec{Stem: "q_java"},
	}
	if _, err := fetchRun(t, env, in); err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	wantFiles(t, cfg.Workdir, map[string]string{"q_java.java": "student\n"})
}

// TestFetchSubmission_AnswerNoMatchKeepsStub asserts that a delivery with
// no root file matching the stem is a no-op for the answer step: the
// teacher stub at the target stays and the activity succeeds.
func TestFetchSubmission_AnswerNoMatchKeepsStub(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/README.txt":   []byte("nothing here\n"),
		"sub/lib/q_java.c": []byte("not at the root\n"),
		"asset/stub.java":  []byte("teacher stub\n"),
	}
	env := newActivityEnv(t, cfg, store)

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
		Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/stub.java", TargetPaths: []string{"src/Solution.java"}}},
		Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "src/Solution.java"},
	}
	if _, err := fetchRun(t, env, in); err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"src/Solution.java": "teacher stub\n",
		"lib/q_java.c":      "not at the root\n",
	})
}

// TestFetchSubmission_AnswerSeveralMatchesByName asserts that when several
// root files match the stem the first by byte-wise name order is the
// answer and the rest stay at the root; failing the run here would punish
// a delivery shape only the student can produce.
func TestFetchSubmission_AnswerSeveralMatchesByName(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/q_java.java": []byte("java\n"),
		"sub/q_java.c":    []byte("c\n"),
		"sub/q_java":      []byte("bare\n"),
		"sub/q_java2.c":   []byte("other stem\n"),
	}
	env := newActivityEnv(t, cfg, store)

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
		Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "answer/Solution"},
	}
	if _, err := fetchRun(t, env, in); err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"answer/Solution": "bare\n",
		"q_java.c":        "c\n",
		"q_java.java":     "java\n",
		"q_java2.c":       "other stem\n",
	})
	wantMissing(t, cfg.Workdir, "q_java")
}

// TestFetchSubmission_ObjectReplacesDeliveredBlockers asserts that a
// delivered directory at an object's target and a delivered file where
// one of its parent directories must be are both removed for the teacher
// file; leaving either would make the delivery shape decide the skeleton.
func TestFetchSubmission_ObjectReplacesDeliveredBlockers(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/pom.xml/inner.txt": []byte("delivered under a directory named pom.xml\n"),
		"sub/src":               []byte("delivered file named src\n"),
		"asset/pom.xml":         []byte("teacher pom\n"),
		"asset/Main.java":       []byte("teacher main\n"),
	}
	env := newActivityEnv(t, cfg, store)

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
		Objects: []contract.ObjectSpec{
			{Bucket: "b", Key: "asset/pom.xml", TargetPaths: []string{"pom.xml"}},
			{Bucket: "b", Key: "asset/Main.java", TargetPaths: []string{"src/main/java/app/Main.java"}},
		},
	}
	if _, err := fetchRun(t, env, in); err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"pom.xml":                     "teacher pom\n",
		"src/main/java/app/Main.java": "teacher main\n",
	})
	wantMissing(t, cfg.Workdir, "pom.xml/inner.txt")
}

// TestFetchSubmission_AnswerReplacesDeliveredBlockers asserts that a
// delivered directory at the answer's target, or a delivered file where a
// parent directory of the target must be, is removed and the answer is
// placed; only a teacher object in the way is a staging failure.
func TestFetchSubmission_AnswerReplacesDeliveredBlockers(t *testing.T) {
	cases := []struct {
		name    string
		objects map[string][]byte
	}{
		{"directory at the target", map[string][]byte{
			"sub/q_java.java": []byte("student\n"),
			"sub/src/main/java/app/Solution.java/x.txt": []byte("delivered directory\n"),
		}},
		{"file where a parent must be", map[string][]byte{
			"sub/q_java.java": []byte("student\n"),
			"sub/src/main":    []byte("delivered file\n"),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			store := newFakeStore()
			store.objects["b"] = tc.objects
			env := newActivityEnv(t, cfg, store)

			in := contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
				Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "src/main/java/app/Solution.java"},
			}
			if _, err := fetchRun(t, env, in); err != nil {
				t.Fatalf("FetchSubmission failed: %v", err)
			}
			wantFiles(t, cfg.Workdir, map[string]string{"src/main/java/app/Solution.java": "student\n"})
			wantMissing(t, cfg.Workdir, "q_java.java")
		})
	}
}

// TestFetchSubmission_StagingInvalid asserts that a teacher object at the
// answer's target as a directory, or as a file where a parent directory
// of the target must be, fails with the contract's non-retryable
// staging-invalid type, and that the same objects are fine when no
// delivered file matches the stem, because there is nothing to place.
func TestFetchSubmission_StagingInvalid(t *testing.T) {
	cases := []struct {
		name   string
		target string
	}{
		{"object directory at the answer target", "src/app/Solution.java/Helper.java"},
		{"object file where a parent of the answer target must be", "src/app"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			store := newFakeStore()
			store.objects["b"] = map[string][]byte{
				"sub/q_java.java": []byte("student\n"),
				"asset/file":      []byte("teacher\n"),
			}
			env := newActivityEnv(t, cfg, store)

			in := contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
				Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/file", TargetPaths: []string{tc.target}}},
				Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "src/app/Solution.java"},
			}
			_, err := fetchRun(t, env, in)
			if err == nil {
				t.Fatal("expected a staging failure, got nil")
			}
			var appErr *temporal.ApplicationError
			if !errors.As(err, &appErr) {
				t.Fatalf("expected *temporal.ApplicationError, got %T: %v", err, err)
			}
			if appErr.Type() != contract.StagingInvalidErrorType {
				t.Errorf("error type = %q, want %q", appErr.Type(), contract.StagingInvalidErrorType)
			}
			if !appErr.NonRetryable() {
				t.Error("staging failure must be non-retryable")
			}
			wantFiles(t, cfg.Workdir, map[string]string{tc.target: "teacher\n"})

			// The same objects with no delivered answer stage cleanly.
			delete(store.objects["b"], "sub/q_java.java")
			cfg2 := newTestConfig(t)
			if _, err := fetchRun(t, newActivityEnv(t, cfg2, store), in); err != nil {
				t.Errorf("without a delivered answer the same objects failed: %v", err)
			}
			wantFiles(t, cfg2.Workdir, map[string]string{tc.target: "teacher\n"})
		})
	}
}

// TestFetchSubmission_MissingObjectKeyFails asserts that an object whose
// key does not exist fails the activity as an ordinary error, not as a
// staging failure, and creates no target file.
func TestFetchSubmission_MissingObjectKeyFails(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, newFakeStore())

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/absent", TargetPaths: []string{"a/b.txt", "c.txt"}}},
	}
	err := func() error { _, err := fetchRun(t, env, in); return err }()
	if err == nil {
		t.Fatal("expected a missing key to fail the activity")
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.Type() == contract.StagingInvalidErrorType {
		t.Errorf("missing key reported as %q; only a teacher clash carries that type", appErr.Type())
	}
	wantMissing(t, cfg.Workdir, "a/b.txt", "c.txt")
}

// TestFetchSubmission_ObjectTargetRejected asserts that an object target
// which escapes the workspace, is absolute, or names the root itself fails
// the activity before anything is written.
func TestFetchSubmission_ObjectTargetRejected(t *testing.T) {
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{"asset/file": []byte("teacher\n")}
	for _, target := range []string{"../escape", "/abs/escape", ".", ""} {
		t.Run(target, func(t *testing.T) {
			cfg := newTestConfig(t)
			env := newActivityEnv(t, cfg, store)
			in := contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersion,
				Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/file", TargetPaths: []string{target}}},
			}
			if _, err := fetchRun(t, env, in); err == nil {
				t.Errorf("expected object target %q to be rejected", target)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Workdir), "escape")); err == nil {
				t.Error("object escaped the workspace")
			}
		})
	}
}

// TestFetchSubmission_AnswerTargetRejected asserts that an answer target
// outside the workspace fails the activity and leaves the delivered file
// out of the tree rather than placing it somewhere else.
func TestFetchSubmission_AnswerTargetRejected(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{"sub/q_java.java": []byte("student\n")}
	env := newActivityEnv(t, cfg, store)

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
		Answer:          &contract.AnswerSpec{Stem: "q_java", TargetPath: "../escape.java"},
	}
	if _, err := fetchRun(t, env, in); err == nil {
		t.Fatal("expected the escaping answer target to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Workdir), "escape.java")); err == nil {
		t.Error("answer escaped the workspace")
	}
}

// TestPlaceFile asserts the preparation of a file target under the
// workspace: parents are created, a file or link on the way and a
// directory or link at the target are removed, a regular file at the
// target is kept for the caller to truncate, and the root and paths
// outside it are rejected.
func TestPlaceFile(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dest, err := placeFile(root, "a/b/c.txt", 0o755)
	if err != nil || dest != filepath.Join(root, "a/b/c.txt") {
		t.Fatalf("placeFile(a/b/c.txt) = %q, %v", dest, err)
	}
	if st, err := os.Stat(filepath.Join(root, "a/b")); err != nil || !st.IsDir() {
		t.Errorf("parents not created: %v", err)
	}

	mustWrite("f", "a file where a directory must be")
	if _, err := placeFile(root, "f/g/h.txt", 0o755); err != nil {
		t.Fatalf("placeFile over a file component: %v", err)
	}
	if st, err := os.Stat(filepath.Join(root, "f/g")); err != nil || !st.IsDir() {
		t.Errorf("file component not replaced by a directory: %v", err)
	}

	mustWrite("d/inner.txt", "a directory at the target")
	if _, err := placeFile(root, "d", 0o755); err != nil {
		t.Fatalf("placeFile over a directory target: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "d")); err == nil {
		t.Error("directory at the target was not removed")
	}

	mustWrite("keep.txt", "kept")
	if _, err := placeFile(root, "keep.txt", 0o755); err != nil {
		t.Fatalf("placeFile over a regular file: %v", err)
	}
	wantFiles(t, root, map[string]string{"keep.txt": "kept"})

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := placeFile(root, "link", 0o755); err != nil {
		t.Fatalf("placeFile over a symlink target: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "link")); err == nil {
		t.Error("symlink at the target was not removed")
	}
	wantFiles(t, filepath.Dir(outside), map[string]string{"outside.txt": "outside"})

	for _, rel := range []string{"", ".", "a/..", "../x", "/abs"} {
		if dest, err := placeFile(root, rel, 0o755); err == nil {
			t.Errorf("placeFile(%q) = %q, want rejection", rel, dest)
		}
	}
}

// TestMoveFile asserts that a move replaces a regular file at the
// destination and that the copy fallback used across filesystems keeps
// the content and permission bits and removes the source.
func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("moved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("replaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	wantFiles(t, dir, map[string]string{"dst": "moved"})
	wantMissing(t, dir, "src")

	if err := os.WriteFile(src, []byte("copied"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "fresh")
	if err := copyFile(src, fresh); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	wantFiles(t, dir, map[string]string{"fresh": "copied", "src": "copied"})
	if st, err := os.Stat(fresh); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("copy created with mode %v, want 0600", st.Mode().Perm())
	}
}

func TestFetchSubmission_TraversalKeyRejected(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/../../escape": []byte("evil"),
	}
	env := newActivityEnv(t, cfg, store)

	input := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersion,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
	}
	if _, err := env.ExecuteActivity(contract.FetchSubmissionActivity, input); err == nil {
		t.Fatal("expected traversal key to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Workdir), "escape")); err == nil {
		t.Fatal("traversal file escaped the workspace")
	}
}

func TestFetchSubmission_TargetDirTraversalRejected(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, newFakeStore())

	for _, target := range []string{"../escape", "/abs/escape"} {
		input := contract.FetchSubmissionInput{
			ProtocolVersion: contract.ProtocolVersion,
			Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "p/", TargetDir: target}},
		}
		if _, err := env.ExecuteActivity(contract.FetchSubmissionActivity, input); err == nil {
			t.Errorf("expected target dir %q to be rejected", target)
		}
	}
}

func execRun(t *testing.T, env *testsuite.TestActivityEnvironment, in contract.RunExecInput) contract.ExecResult {
	t.Helper()
	val, err := env.ExecuteActivity(contract.RunExecActivity, in)
	if err != nil {
		t.Fatalf("RunExec failed: %v", err)
	}
	var res contract.ExecResult
	if err := val.Get(&res); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	return res
}

func TestRunExec_HappyPath(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Stage:           "test",
		ScenarioCode:    "q1",
		Spec: contract.ExecSpec{
			Command:    "/bin/echo",
			Args:       []string{"hello"},
			StdoutPath: strPtr("out/echo.txt"),
			Workdir:    ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q1"},
	}
	res := execRun(t, env, input)

	if res.Command != "/bin/echo" || !reflect.DeepEqual(res.Args, []string{"hello"}) {
		t.Errorf("command echo = %q %v", res.Command, res.Args)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want empty", res.Error)
	}

	if data, ok := store.upload("runs", "55/test/q1/stdout"); !ok || string(data) != "hello\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "hello\n")
	}
	if data, ok := store.upload("runs", "55/test/q1/stderr"); !ok || len(data) != 0 {
		t.Errorf("uploaded stderr = %q (present=%v), want empty object", data, ok)
	}
	if want := contract.URIFor("runs", "55/test/q1/stdout"); res.StdoutURI != want {
		t.Errorf("StdoutURI = %q, want %q", res.StdoutURI, want)
	}
	if want := contract.URIFor("runs", "55/test/q1/stderr"); res.StderrURI != want {
		t.Errorf("StderrURI = %q, want %q", res.StderrURI, want)
	}
	if res.StdinURI != "" {
		t.Errorf("StdinURI = %q, want empty (no stdin provided)", res.StdinURI)
	}

	// StdoutPath copy lands relative to the effective workdir.
	data, err := os.ReadFile(filepath.Join(cfg.Workdir, "out/echo.txt"))
	if err != nil {
		t.Fatalf("stdout_path copy missing: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("stdout_path copy = %q, want %q", data, "hello\n")
	}

	if res.StartedAt.IsZero() || res.EndedAt.IsZero() || res.EndedAt.Before(res.StartedAt) {
		t.Errorf("timestamps invalid: started=%v ended=%v", res.StartedAt, res.EndedAt)
	}
	if res.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want >= 0", res.DurationMs)
	}
}

func TestRunExec_NonZeroExit(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/false", Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q2"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 1 {
		t.Fatalf("ExitCode = %v, want 1", res.ExitCode)
	}
	if res.Error != "" {
		t.Errorf("Error = %q, want empty (non-zero exit is not an error)", res.Error)
	}
	if res.TimedOut {
		t.Error("TimedOut = true, want false")
	}
	if res.StdoutURI == "" || res.StderrURI == "" {
		t.Error("stdout/stderr URIs must be populated even on failure")
	}
}

func TestRunExec_Timeout(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:   "/bin/sleep",
			Args:      []string{"5"},
			TimeoutMs: 200,
			Workdir:   ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q3"},
	}
	start := time.Now()
	res := execRun(t, env, input)
	elapsed := time.Since(start)

	if !res.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}
	if elapsed > 4*time.Second {
		t.Errorf("activity took %v; the process group was not killed at the deadline", elapsed)
	}
	if res.ExitCode == nil {
		t.Error("ExitCode = nil, want the kill status from ProcessState")
	}
	if res.StdoutURI == "" || res.StderrURI == "" {
		t.Error("stdout/stderr URIs must be populated on timeout")
	}
}

func TestRunExec_EnvScrub(t *testing.T) {
	// The agent's own credentials must never leak into a student
	// process; Spec.Env must be visible.
	t.Setenv("GHOST_AGENT_TEMPORAL_ADDRESS", "temporal.internal:7233")
	t.Setenv("GHOST_AGENT_STORAGE_SECRET_KEY", "super-secret")

	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command: "/usr/bin/env",
			Env:     map[string]string{"SPEC_VAR": "spec-value"},
			Workdir: ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q4"},
	}
	res := execRun(t, env, input)
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}

	data, ok := store.upload("runs", "55/test/q4/stdout")
	if !ok {
		t.Fatal("stdout was not uploaded")
	}
	out := string(data)
	if strings.Contains(out, "GHOST_AGENT_") {
		t.Errorf("child environment leaked GHOST_AGENT_* variables:\n%s", out)
	}
	if !strings.Contains(out, "SPEC_VAR=spec-value") {
		t.Errorf("child environment missing Spec.Env entry SPEC_VAR:\n%s", out)
	}
}

func TestRunExec_StdinContent(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:      "/bin/cat",
			StdinContent: strPtr("hello agent"),
			Workdir:      ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q5"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	if data, ok := store.upload("runs", "55/test/q5/stdout"); !ok || string(data) != "hello agent" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "hello agent")
	}
	if data, ok := store.upload("runs", "55/test/q5/stdin"); !ok || string(data) != "hello agent" {
		t.Errorf("uploaded stdin = %q (present=%v), want %q", data, ok, "hello agent")
	}
	if want := contract.URIFor("runs", "55/test/q5/stdin"); res.StdinURI != want {
		t.Errorf("StdinURI = %q, want %q", res.StdinURI, want)
	}
}

func TestRunExec_StdinPath(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.WriteFile(filepath.Join(cfg.Workdir, "q.in"), []byte("42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec: contract.ExecSpec{
			Command:   "/bin/cat",
			StdinPath: strPtr("q.in"),
			Workdir:   ".",
		},
		StdioUpload: contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q6"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0 (error: %q)", res.ExitCode, res.Error)
	}
	if data, ok := store.upload("runs", "55/test/q6/stdout"); !ok || string(data) != "42\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "42\n")
	}
	if data, ok := store.upload("runs", "55/test/q6/stdin"); !ok || string(data) != "42\n" {
		t.Errorf("uploaded stdin = %q (present=%v), want %q", data, ok, "42\n")
	}
	if want := contract.URIFor("runs", "55/test/q6/stdin"); res.StdinURI != want {
		t.Errorf("StdinURI = %q, want %q", res.StdinURI, want)
	}
}

func TestRunExec_SpawnFailure(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.GhostPath = "/nonexistent/ghost-binary"
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Args: []string{"hi"}, Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q7"},
	}
	res := execRun(t, env, input)

	if res.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil for a spawn failure", *res.ExitCode)
	}
	if res.Error == "" {
		t.Error("Error must explain the spawn failure")
	}
	if res.StdoutURI != "" || res.StderrURI != "" || res.StdinURI != "" {
		t.Error("no stdio URIs should be set when the command never spawned")
	}
}

func TestRunExec_WorkdirEscapeRejected(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Workdir: "../outside"},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q8"},
	}
	res := execRun(t, env, input)

	if res.ExitCode != nil {
		t.Errorf("ExitCode = %v, want nil", *res.ExitCode)
	}
	if !strings.Contains(res.Error, "escapes the workspace") {
		t.Errorf("Error = %q, want a workspace-escape rejection", res.Error)
	}
}

func TestRunExec_UploadFailureSurfacesOnError(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.uploadErr = errors.New("storage unreachable")
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Args: []string{"hi"}, Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/q9"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", res.ExitCode)
	}
	if !strings.Contains(res.Error, "storage unreachable") {
		t.Errorf("Error = %q, want the upload failure", res.Error)
	}
	if res.StdoutURI != "" || res.StderrURI != "" {
		t.Error("failed uploads must not yield URIs")
	}
}

// TestRunExec_Sandboxed exercises the full sandboxed child path
// (Landlock + netns + RLIMIT_NPROC). It needs a kernel with Landlock
// and unprivileged user namespaces, so it only runs when
// GHOST_TEST_SANDBOX=1 is set.
func TestRunExec_Sandboxed(t *testing.T) {
	if os.Getenv("GHOST_TEST_SANDBOX") != "1" {
		t.Skip("set GHOST_TEST_SANDBOX=1 to run the sandboxed child test")
	}

	cfg := newTestConfig(t)
	cfg.Sandbox = true
	cfg.MaxPids = 64
	store := newFakeStore()
	env := newActivityEnv(t, cfg, store)

	input := contract.RunExecInput{
		ProtocolVersion: contract.ProtocolVersion,
		Spec:            contract.ExecSpec{Command: "/bin/echo", Args: []string{"sandboxed"}, Workdir: "."},
		StdioUpload:     contract.StdioUploadSpec{Bucket: "runs", KeyPrefix: "55/test/sandbox"},
	}
	res := execRun(t, env, input)

	if res.ExitCode == nil || *res.ExitCode != 0 {
		stderr, _ := store.upload("runs", "55/test/sandbox/stderr")
		t.Fatalf("ExitCode = %s, want 0 (error: %q, child stderr: %q)", fmtExitCode(res.ExitCode), res.Error, stderr)
	}
	if data, ok := store.upload("runs", "55/test/sandbox/stdout"); !ok || string(data) != "sandboxed\n" {
		t.Errorf("uploaded stdout = %q (present=%v), want %q", data, ok, "sandboxed\n")
	}
}

func fmtExitCode(code *int) string {
	if code == nil {
		return "<nil>"
	}
	return strconv.Itoa(*code)
}
