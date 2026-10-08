package agent

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zinc-sig/ghost/internal/agent/contract"
)

// refusedErrorKind returns ErrorKindCommand for an error that wraps one of
// the sentinels the agent puts on a failure the run's commands cause
// (errCaptureRefused, errWorkspacePathRefused), and ErrorKindInfra for any
// other. Those sentinels cover every shape a command can leave at a path the
// agent opens: a symbolic link, a directory, a FIFO, a file where a
// directory belongs, or a mode the agent cannot write through. What is left
// is the agent's own failure, such as a full disk or its staging session
// gone missing; the staging area is outside every path a command may
// write, so nothing there is the command's doing.
func refusedErrorKind(err error) contract.ErrorKind {
	if errors.Is(err, errCaptureRefused) || errors.Is(err, errWorkspacePathRefused) {
		return contract.ErrorKindCommand
	}
	return contract.ErrorKindInfra
}

// workdirErrorKind classifies a failure to create the effective workdir,
// which securePathUnder already placed inside the workspace.
func workdirErrorKind(err error) contract.ErrorKind {
	return refusedErrorKind(err)
}

// captureCopyErrorKind classifies a failed copy of a stdio capture to rel, a
// destination relative to workdir. A destination outside the workspace, or
// one naming the workdir itself, is the spec's fault and a rerun repeats it.
func captureCopyErrorKind(root, workdir, rel string, err error) contract.ErrorKind {
	dest, perr := securePathUnder(root, workdir, rel)
	if perr != nil || dest == workdir {
		return contract.ErrorKindCommand
	}
	return refusedErrorKind(err)
}

// stdinErrorKind classifies a failure to stage the spec's stdin path p. A
// path outside the workspace or naming its root is the spec's fault, and a
// missing file under a workspace that exists is one the spec names and the
// run's commands did not leave; a rerun repeats both. A failure writing the
// staged copy is a *fs.PathError on the staging path, which is the agent's
// own.
func stdinErrorKind(root, p string, err error) contract.ErrorKind {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && !pathWithin(root, pathErr.Path) {
		return contract.ErrorKindInfra
	}
	if kind := refusedErrorKind(err); kind == contract.ErrorKindCommand {
		return kind
	}
	absRoot, aerr := filepath.Abs(root)
	if aerr != nil {
		return contract.ErrorKindInfra
	}
	rel := p
	if filepath.IsAbs(p) {
		rel, aerr = filepath.Rel(absRoot, filepath.Clean(p))
		if aerr != nil {
			return contract.ErrorKindInfra
		}
	}
	if src, perr := securePathUnder(absRoot, absRoot, rel); perr != nil || src == absRoot {
		return contract.ErrorKindCommand
	}
	if errors.Is(err, fs.ErrNotExist) {
		if st, serr := os.Stat(absRoot); serr == nil && st.IsDir() {
			return contract.ErrorKindCommand
		}
	}
	return contract.ErrorKindInfra
}

// pathWithin reports whether p is root or a path under it, compared
// lexically after making both absolute.
func pathWithin(root, p string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absP)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
