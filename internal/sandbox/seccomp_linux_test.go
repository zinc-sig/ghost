//go:build linux

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/elastic/go-seccomp-bpf/arch"
	"golang.org/x/net/bpf"
)

// denyLinkProfile is a minimal profile (default allow) that denies the four
// link-creation syscalls — the same shape core's full allowlist enforces by
// omission. Used to test the apply mechanism without risking the Go runtime
// under a default-deny filter.
const denyLinkProfile = `{
  "defaultAction": "SCMP_ACT_ALLOW",
  "architectures": ["SCMP_ARCH_AARCH64", "SCMP_ARCH_X86_64", "SCMP_ARCH_X86"],
  "syscalls": [
    {"names": ["symlink", "symlinkat", "link", "linkat"], "action": "SCMP_ACT_ERRNO"}
  ]
}`

const (
	seccompChildEnv = "GHOST_SECCOMP_TEST_CHILD"
	seccompJSONEnv  = "GHOST_SECCOMP_TEST_JSON"
)

// TestApplySeccompFromJSON_DeniesLink passes the profile inline as JSON (the
// single-source delivery path core uses), applies it in a forked child so the
// filter never touches the parent test runtime, attempts a symlink, and asserts
// it is denied.
func TestApplySeccompFromJSON_DeniesLink(t *testing.T) {
	if os.Getenv(seccompChildEnv) == "1" {
		// CHILD: apply the inline profile, attempt a symlink, report via exit.
		if err := ApplySeccompFromJSON([]byte(os.Getenv(seccompJSONEnv))); err != nil {
			t.Fatalf("ApplySeccompFromJSON: %v", err)
		}
		target := filepath.Join(t.TempDir(), "symlink-target")
		if err := os.Symlink("/etc/hostname", target); err != nil {
			os.Exit(0) // denied — expected.
		}
		os.Exit(1) // not denied — the filter failed.
	}

	// PARENT: pass the JSON inline via env (no file), fork the child.
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplySeccompFromJSON_DeniesLink$")
	cmd.Env = append(os.Environ(),
		seccompChildEnv+"=1",
		seccompJSONEnv+"="+denyLinkProfile,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("link was NOT denied after applying the seccomp profile (child did not exit 0)\nchild output:\n%s\nerr: %v", out, err)
	}
}

// TestBuildProgram_DefaultDenyWithConditional validates the generator handles
// core's real profile shape: default-deny (ERRNO + defaultErrnoRet), an allow
// list, and a conditional entry (the clone CLONE_THREAD mask). It only builds
// and assembles the BPF (no Load), so it is safe to run in-process, and asserts
// every conditional jump stayed within BPF's 8-bit skip range.
func TestBuildProgram_DefaultDenyWithConditional(t *testing.T) {
	errno := uint(38) // ENOSYS, matching core's defaultErrnoRet.
	profile := &seccompProfile{
		DefaultAction:   "SCMP_ACT_ERRNO",
		DefaultErrnoRet: &errno,
		Architectures:   []string{"SCMP_ARCH_AARCH64", "SCMP_ARCH_X86_64", "SCMP_ARCH_X86"},
		Syscalls: []seccompSyscall{
			{Names: []string{"read", "write", "exit", "exit_group", "rt_sigreturn"}, Action: "SCMP_ACT_ALLOW"},
			{Names: []string{"clone"}, Action: "SCMP_ACT_ALLOW", Args: []seccompArg{
				{Index: 0, Value: 0x10000, ValueTwo: 0x10000, Op: "SCMP_CMP_MASKED_EQ"},
			}},
		},
	}
	insts, err := buildProgram(profile)
	if err != nil {
		t.Fatalf("buildProgram failed on a core-shaped profile: %v", err)
	}
	if len(insts) == 0 {
		t.Fatal("buildProgram returned no instructions")
	}
	// The whole point of the cgo-free assembler is that it produces a valid,
	// loadable BPF program; bpf.Assemble enforces skip-range and shape.
	if _, err := bpf.Assemble(insts); err != nil {
		t.Fatalf("assembled program is not valid BPF: %v", err)
	}
}

// TestBuildProgram_LargeAllowlistFitsShortJumps guards the generator invariant
// that conditional jumps never exceed BPF's 8-bit skip field regardless of
// allowlist size — the failure mode that a naive one-cell-per-syscall design
// would hit. It builds a ~400-entry allowlist (larger than core's real one).
func TestBuildProgram_LargeAllowlistFitsShortJumps(t *testing.T) {
	names := make([]string, 0, 400)
	// Use real x86_64 names so they resolve; repeat the core allowlist-ish set.
	base := []string{"read", "write", "open", "close", "stat", "fstat", "lstat",
		"poll", "lseek", "mmap", "mprotect", "munmap", "brk", "rt_sigaction",
		"rt_sigprocmask", "ioctl", "pread64", "pwrite64", "readv", "writev",
		"access", "pipe", "select", "sched_yield", "mremap", "msync", "mincore",
		"madvise", "dup", "dup2", "nanosleep", "getpid", "socket", "connect",
		"accept", "sendto", "recvfrom", "bind", "listen", "getsockname"}
	for len(names) < 400 {
		names = append(names, base...)
	}
	profile := &seccompProfile{
		DefaultAction: "SCMP_ACT_ERRNO",
		Architectures: []string{"SCMP_ARCH_X86_64"},
		Syscalls:      []seccompSyscall{{Names: names, Action: "SCMP_ACT_ALLOW"}},
	}
	insts, err := buildProgram(profile)
	if err != nil {
		t.Fatalf("buildProgram failed on a large allowlist: %v", err)
	}
	if _, err := bpf.Assemble(insts); err != nil {
		t.Fatalf("large-allowlist program is not valid BPF: %v", err)
	}
}

// TestResolveSyscall_NewfstatatAliases guards the libseccomp name-alias layer:
// core's Docker profile lists newfstatat (glibc's runtime stat), but elastic's
// tables spell that syscall fstatat on aarch64 and fstatat64 on i386/arm. If the
// alias resolution regresses, newfstatat silently drops on those arches and a
// default-deny profile denies libc's stat, breaking the dynamic loader. Each
// arch here must resolve newfstatat to its real syscall number.
func TestResolveSyscall_NewfstatatAliases(t *testing.T) {
	for _, tc := range []struct {
		name string
		info *arch.Info
	}{
		{"x86_64", arch.X86_64},
		{"i386", arch.I386},
		{"aarch64", arch.AARCH64},
		{"arm", arch.ARM},
	} {
		nr, ok := resolveSyscall(tc.info, "newfstatat")
		if !ok {
			t.Errorf("%s: newfstatat did not resolve (would drop to default-deny and break libc)", tc.name)
			continue
		}
		if nr < 0 {
			t.Errorf("%s: newfstatat resolved to invalid number %d", tc.name, nr)
		}
	}
}

// TestApplySeccompFromJSON_EmptyIsNoOp confirms the gating: empty input means
// no filter (backward-compatible when the flag isn't passed).
func TestApplySeccompFromJSON_EmptyIsNoOp(t *testing.T) {
	if err := ApplySeccompFromJSON(nil); err != nil {
		t.Fatalf("empty input should be a no-op, got: %v", err)
	}
}

// TestBuildProgram_UnknownArchErrors guards defect fix #1: an unrecognized
// architecture name must fail fast, not be silently skipped. Skipping is unsafe
// because if every declared arch is unknown, zero arch blocks are emitted and the
// filter degenerates to a bare RET defaultAction — a default-ALLOW profile then
// installs an inert filter that silently drops all its intended denials.
func TestBuildProgram_UnknownArchErrors(t *testing.T) {
	// All-unknown: the dangerous case that would collapse to an empty filter.
	onlyUnknown := &seccompProfile{
		DefaultAction: "SCMP_ACT_ALLOW",
		Architectures: []string{"SCMP_ARCH_BOGUS"},
		Syscalls:      []seccompSyscall{{Names: []string{"symlink"}, Action: "SCMP_ACT_ERRNO"}},
	}
	if _, err := buildProgram(onlyUnknown); err == nil {
		t.Fatal("expected error for an unrecognized architecture, got nil (an empty/inert filter would be installed)")
	}

	// Mixed known+unknown: still fail fast — a typo must not slip through just
	// because other arches are valid.
	mixed := &seccompProfile{
		DefaultAction: "SCMP_ACT_ALLOW",
		Architectures: []string{"SCMP_ARCH_X86_64", "SCMP_ARCH_TYPO"},
		Syscalls:      []seccompSyscall{{Names: []string{"symlink"}, Action: "SCMP_ACT_ERRNO"}},
	}
	if _, err := buildProgram(mixed); err == nil {
		t.Fatal("expected error when any declared architecture is unrecognized, got nil")
	}

	// Sanity: the same profile with only recognized arches must still build.
	ok := &seccompProfile{
		DefaultAction: "SCMP_ACT_ALLOW",
		Architectures: []string{"SCMP_ARCH_X86_64"},
		Syscalls:      []seccompSyscall{{Names: []string{"symlink"}, Action: "SCMP_ACT_ERRNO"}},
	}
	if _, err := buildProgram(ok); err != nil {
		t.Fatalf("a profile with only recognized arches must build, got: %v", err)
	}
}

// TestBuildProgram_ArgIndexOutOfRangeErrors guards defect fix #2: an argument
// index beyond args[5] must be rejected. Left unchecked, argOffsets' uint32
// arithmetic (16 + 8*index) can wrap a huge index back to a small in-bounds
// offset, so seccomp(2) would SUCCEED while the filter compares the wrong
// argument — a silent policy change.
func TestBuildProgram_ArgIndexOutOfRangeErrors(t *testing.T) {
	profile := &seccompProfile{
		DefaultAction: "SCMP_ACT_ERRNO",
		Architectures: []string{"SCMP_ARCH_X86_64"},
		Syscalls: []seccompSyscall{
			{Names: []string{"clone"}, Action: "SCMP_ACT_ALLOW", Args: []seccompArg{
				{Index: 6, Value: 0, Op: "SCMP_CMP_EQ"},
			}},
		},
	}
	if _, err := buildProgram(profile); err == nil {
		t.Fatal("expected error for arg index 6 (seccomp_data has only args[0..5]), got nil")
	}

	// The overflow-wrap case: 8*index wraps uint32 back to a small offset.
	wrap := &seccompProfile{
		DefaultAction: "SCMP_ACT_ERRNO",
		Architectures: []string{"SCMP_ARCH_X86_64"},
		Syscalls: []seccompSyscall{
			{Names: []string{"clone"}, Action: "SCMP_ACT_ALLOW", Args: []seccompArg{
				{Index: 0x20000000, Value: 0, Op: "SCMP_CMP_EQ"},
			}},
		},
	}
	if _, err := buildProgram(wrap); err == nil {
		t.Fatal("expected error for an out-of-range arg index that overflows the offset computation, got nil")
	}
}
