package agent

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.temporal.io/sdk/temporal"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// placementStore holds a part-enabled delivery (two matched files and one
// unmatched file), a teacher skeleton, and a teacher mount.
func placementStore() *fakeStore {
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/q1/Solution.java": []byte("student solution\n"),
		"sub/report.pdf":       []byte("student report\n"),
		"sub/notes.txt":        []byte("unmatched\n"),
		"asset/Solution.java":  []byte("teacher stub\n"),
		"asset/Main.java":      []byte("teacher main\n"),
		"asset/pom.xml":        []byte("teacher pom\n"),
		"exp/out.txt":          []byte("teacher expected\n"),
	}
	return store
}

// placementInput is a part-enabled run: no delivery mirror and no answer,
// a teacher mount, a teacher skeleton with a stub at the solution path, and
// one student placement per matched file.
func placementInput() contract.FetchSubmissionInput {
	return contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersionPlacements,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "exp/", TargetDir: "expected"}},
		Objects: []contract.ObjectSpec{
			{Bucket: "b", Key: "asset/Solution.java", TargetPaths: []string{"src/app/Solution.java"}},
			{Bucket: "b", Key: "asset/Main.java", TargetPaths: []string{"src/app/Main.java"}},
		},
		Placements: []contract.PlacementSpec{
			{Bucket: "b", Key: "sub/q1/Solution.java", TargetPath: "src/app/Solution.java"},
			{Bucket: "b", Key: "sub/report.pdf", TargetPath: "report.pdf"},
		},
	}
}

// TestFetchSubmission_PlacementReplacesSkeletonStub asserts that a student
// placement over a teacher object at its exact path replaces the stub, that
// the rest of the skeleton and the mount stay, that a delivered file with
// no placement never reaches the workspace, and that the result counts
// every placed file. A placement that lost to the stub would grade the
// teacher's skeleton as the student's work.
func TestFetchSubmission_PlacementReplacesSkeletonStub(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, placementStore())

	res, err := fetchRun(t, env, placementInput())
	if err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	want := contract.FetchSubmissionResult{
		AgentProtocolVersion: contract.ProtocolVersionPlacements,
		AgentVersion:         "test",
		Files:                5,
		Bytes: int64(len("teacher expected\n") + len("teacher stub\n") + len("teacher main\n") +
			len("student solution\n") + len("student report\n")),
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v, want %+v", res, want)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"src/app/Solution.java": "student solution\n",
		"src/app/Main.java":     "teacher main\n",
		"report.pdf":            "student report\n",
		"expected/out.txt":      "teacher expected\n",
	})
	wantMissing(t, cfg.Workdir, "notes.txt", "q1", "sub")
}

// TestFetchSubmission_PlacementReplacesMountedFile asserts that a placement
// at the exact path of a file mirrored from a teacher mount replaces it,
// the same as a teacher object at that path.
func TestFetchSubmission_PlacementReplacesMountedFile(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, placementStore())

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersionPlacements,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "exp/", TargetDir: "expected"}},
		Placements:      []contract.PlacementSpec{{Bucket: "b", Key: "sub/report.pdf", TargetPath: "expected/out.txt"}},
	}
	if _, err := fetchRun(t, env, in); err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	wantFiles(t, cfg.Workdir, map[string]string{"expected/out.txt": "student report\n"})
}

// TestFetchSubmission_PlacementProtectsTeacherFiles asserts that a
// placement which would destroy a teacher directory, or which needs a
// directory where a teacher file sits, is the non-retryable staging
// failure for teacher objects and mounted files alike, and leaves the
// teacher file in place.
func TestFetchSubmission_PlacementProtectsTeacherFiles(t *testing.T) {
	cases := []struct {
		name   string
		target string
		intact map[string]string
	}{
		{"on an object directory", "src/app", map[string]string{"src/app/Main.java": "teacher main\n"}},
		{"below an object file", "pom.xml/Solution.java", map[string]string{"pom.xml": "teacher pom\n"}},
		{"on a mounted directory", "expected", map[string]string{"expected/out.txt": "teacher expected\n"}},
		{"below a mounted file", "expected/out.txt/Solution.java", map[string]string{"expected/out.txt": "teacher expected\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			env := newActivityEnv(t, cfg, placementStore())
			in := contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersionPlacements,
				Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "exp/", TargetDir: "expected"}},
				Objects: []contract.ObjectSpec{
					{Bucket: "b", Key: "asset/Main.java", TargetPaths: []string{"src/app/Main.java"}},
					{Bucket: "b", Key: "asset/pom.xml", TargetPaths: []string{"pom.xml"}},
				},
				Placements: []contract.PlacementSpec{{Bucket: "b", Key: "sub/q1/Solution.java", TargetPath: tc.target}},
			}
			_, err := fetchRun(t, env, in)
			wantStagingInvalid(t, err)
			wantFiles(t, cfg.Workdir, tc.intact)
		})
	}
}

// TestFetchSubmission_PlacementReplacesStudentBlockers asserts that a
// student file or directory in a placement's way is removed rather than
// failing staging: a delivered directory at the target, a delivered file
// where a parent directory must be, and the answer at the same target. The
// placement is written last, so it wins over the answer too.
func TestFetchSubmission_PlacementReplacesStudentBlockers(t *testing.T) {
	cfg := newTestConfig(t)
	store := newFakeStore()
	store.objects["b"] = map[string][]byte{
		"sub/q1.py":           []byte("answer\n"),
		"sub/dir/inner.txt":   []byte("delivered inner\n"),
		"sub/file":            []byte("delivered file\n"),
		"parts/main.py":       []byte("placed main\n"),
		"parts/dir.txt":       []byte("placed dir\n"),
		"parts/file/deep.txt": []byte("placed deep\n"),
	}
	env := newActivityEnv(t, cfg, store)

	in := contract.FetchSubmissionInput{
		ProtocolVersion: contract.ProtocolVersionPlacements,
		Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
		Answer:          &contract.AnswerSpec{Stem: "q1", TargetPath: "src/main.py"},
		Placements: []contract.PlacementSpec{
			{Bucket: "b", Key: "parts/main.py", TargetPath: "src/main.py"},
			{Bucket: "b", Key: "parts/dir.txt", TargetPath: "dir"},
			{Bucket: "b", Key: "parts/file/deep.txt", TargetPath: "file/deep.txt"},
		},
	}
	if _, err := fetchRun(t, env, in); err != nil {
		t.Fatalf("FetchSubmission failed: %v", err)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"src/main.py":   "placed main\n",
		"dir":           "placed dir\n",
		"file/deep.txt": "placed deep\n",
	})
	wantMissing(t, cfg.Workdir, "q1.py")
}

// TestFetchSubmission_ReplacedTeacherPathIsStudent asserts that a teacher
// file the answer or an earlier placement replaced at its exact path stops
// counting as a teacher file, so a later placement that needs a directory
// there removes the student file instead of failing staging. Counting it
// would fail a run with GhostStagingInvalid over a file the student owns.
func TestFetchSubmission_ReplacedTeacherPathIsStudent(t *testing.T) {
	cases := []struct {
		name string
		in   contract.FetchSubmissionInput
	}{
		{
			"the answer replaced the stub",
			contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersionPlacements,
				Downloads:       []contract.DownloadSpec{{Bucket: "b", Prefix: "sub/", TargetDir: "."}},
				Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/stub", TargetPaths: []string{"src/main.py"}}},
				Answer:          &contract.AnswerSpec{Stem: "q1", TargetPath: "src/main.py"},
				Placements:      []contract.PlacementSpec{{Bucket: "b", Key: "parts/deep.txt", TargetPath: "src/main.py/deep.txt"}},
			},
		},
		{
			"an earlier placement replaced the stub",
			contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersionPlacements,
				Objects:         []contract.ObjectSpec{{Bucket: "b", Key: "asset/stub", TargetPaths: []string{"src/main.py"}}},
				Placements: []contract.PlacementSpec{
					{Bucket: "b", Key: "parts/main.py", TargetPath: "src/main.py"},
					{Bucket: "b", Key: "parts/deep.txt", TargetPath: "src/main.py/deep.txt"},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			store := newFakeStore()
			store.objects["b"] = map[string][]byte{
				"sub/q1.py":      []byte("answer\n"),
				"asset/stub":     []byte("teacher stub\n"),
				"parts/main.py":  []byte("placed main\n"),
				"parts/deep.txt": []byte("placed deep\n"),
			}
			env := newActivityEnv(t, cfg, store)
			if _, err := fetchRun(t, env, tc.in); err != nil {
				t.Fatalf("FetchSubmission failed: %v", err)
			}
			wantFiles(t, cfg.Workdir, map[string]string{"src/main.py/deep.txt": "placed deep\n"})
		})
	}
}

// TestFetchSubmission_PlacementTargetRejected asserts that a placement
// target which escapes the workspace, is absolute, or names the root fails
// the activity and writes nothing: the workspace stays empty, and the
// escaping target leaves no file beside it.
func TestFetchSubmission_PlacementTargetRejected(t *testing.T) {
	for _, target := range []string{"../escape", "/abs/escape", ".", ""} {
		t.Run(target, func(t *testing.T) {
			cfg := newTestConfig(t)
			env := newActivityEnv(t, cfg, placementStore())
			in := contract.FetchSubmissionInput{
				ProtocolVersion: contract.ProtocolVersionPlacements,
				Placements:      []contract.PlacementSpec{{Bucket: "b", Key: "sub/report.pdf", TargetPath: target}},
			}
			if _, err := fetchRun(t, env, in); err == nil {
				t.Errorf("expected placement target %q to be rejected", target)
			}
			if entries, err := os.ReadDir(cfg.Workdir); err != nil || len(entries) != 0 {
				t.Errorf("workspace holds %d entries (err %v), want none", len(entries), err)
			}
			if target == "../escape" {
				if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.Workdir), "escape")); err == nil {
					t.Error("placement escaped the workspace")
				}
			}
		})
	}
}

// TestFetchSubmission_PlacementMissingKeyIsRetryable asserts that a
// placement key missing from storage fails the activity with a retryable
// error rather than the staging failure, because core reports the staging
// failure as a problem of the marking scheme, and that it fails before the
// target is prepared, so the teacher stub there is untouched.
func TestFetchSubmission_PlacementMissingKeyIsRetryable(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, placementStore())

	in := placementInput()
	in.Placements = []contract.PlacementSpec{{Bucket: "b", Key: "sub/absent", TargetPath: "src/app/Solution.java"}}
	_, err := fetchRun(t, env, in)
	if err == nil {
		t.Fatal("expected a missing placement key to fail the activity")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *temporal.ApplicationError, got %T: %v", err, err)
	}
	if appErr.Type() == contract.StagingInvalidErrorType {
		t.Errorf("a missing placement key is %s, want a plain error", contract.StagingInvalidErrorType)
	}
	if appErr.NonRetryable() {
		t.Error("a missing placement key must stay retryable")
	}
	wantFiles(t, cfg.Workdir, map[string]string{"src/app/Solution.java": "teacher stub\n"})
}

// TestFetchSubmission_PlacementRetryIsIdempotent asserts that a second
// attempt over the workspace the first one left produces the same result
// and tree, and that no staging session outlives its attempt.
func TestFetchSubmission_PlacementRetryIsIdempotent(t *testing.T) {
	cfg := newTestConfig(t)
	env := newActivityEnv(t, cfg, placementStore())

	first, err := fetchRun(t, env, placementInput())
	if err != nil {
		t.Fatalf("first attempt failed: %v", err)
	}
	second, err := fetchRun(t, env, placementInput())
	if err != nil {
		t.Fatalf("second attempt failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second attempt result = %+v, want %+v", second, first)
	}
	wantFiles(t, cfg.Workdir, map[string]string{
		"src/app/Solution.java": "student solution\n",
		"src/app/Main.java":     "teacher main\n",
		"report.pdf":            "student report\n",
	})
	if entries, _ := os.ReadDir(cfg.StagingDir); len(entries) != 0 {
		t.Errorf("staging dir still holds %d entries after the attempts", len(entries))
	}
}

// TestFetchSubmission_ObjectsInputServedAtEveryVersion asserts that an
// input with objects and an answer and no placements stages the same tree
// at the version that introduced those fields and at the newest version,
// so a core that keeps sending the objects version is served unchanged.
func TestFetchSubmission_ObjectsInputServedAtEveryVersion(t *testing.T) {
	for version := contract.ProtocolVersionObjects; version <= contract.ProtocolVersion; version++ {
		cfg := newTestConfig(t)
		env := newActivityEnv(t, cfg, stagingStore())
		in := stagingInput()
		in.ProtocolVersion = version
		res, err := fetchRun(t, env, in)
		if err != nil {
			t.Fatalf("v%d: FetchSubmission failed: %v", version, err)
		}
		if res.AgentProtocolVersion != version || res.Files != 5 {
			t.Errorf("v%d: result = %+v, want the echoed version and 5 files", version, res)
		}
		wantFiles(t, cfg.Workdir, map[string]string{
			"src/main/java/app/Main.java":     "teacher main\n",
			"src/main/java/app/Solution.java": "student solution\n",
			"pom.xml":                         "teacher pom\n",
		})
		wantMissing(t, cfg.Workdir, "q_java.java")
	}
}
