package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// errCaptureRefused marks a stdout_path or stderr_path copy refused
// because of what the command left on disk: a symbolic link, a directory,
// a FIFO, or a file where a parent directory belongs, at the destination
// or in place of its own capture file. The command controls both paths, so
// the failure is caused by the command and a rerun of the exec on the same
// input fails the same way.
var errCaptureRefused = errors.New("capture copy refused")

// openCapture opens a stdio capture file for reading without following a
// symbolic link and requires a regular file. With the sandbox on, the
// staging area is outside every path a command may write; with it off, or
// without Landlock, same-user command code can reach the staging session
// and could otherwise replace its capture with a link to a file that the
// agent can read and the command cannot.
func openCapture(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, pathError(errCaptureRefused, filepath.Base(path), "the capture", err)
	}
	return regularFile(fd, path, filepath.Base(path), "the capture", errCaptureRefused)
}

// errWorkspacePathRefused marks a stdin path or workdir refused because of
// what a command left in the workspace: a symbolic link, a FIFO, or a file
// where a directory belongs. Like errCaptureRefused, the failure is caused
// by the commands of the run, not by the agent.
var errWorkspacePathRefused = errors.New("workspace path refused")

// splitBeneath splits rel, a clean relative path, into its components.
func splitBeneath(rel string) ([]string, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return nil, fmt.Errorf("path %q is not a clean relative path", rel)
		}
	}
	return parts, nil
}

// walkBeneath opens the directory reached from root through dirs, one
// component at a time with O_NOFOLLOW, and returns its descriptor. With
// create set, a missing component is created with dirMode. A failure the
// workspace's contents cause wraps refused.
func walkBeneath(root string, dirs []string, create bool, dirMode os.FileMode, refused error) (int, error) {
	dirfd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("failed to open workspace root %s: %w", root, err)
	}
	for i, name := range dirs {
		dir := strings.Join(dirs[:i+1], "/")
		if create {
			if err := unix.Mkdirat(dirfd, name, uint32(dirMode.Perm())); err != nil && !errors.Is(err, unix.EEXIST) {
				_ = unix.Close(dirfd)
				return -1, pathError(refused, dir, "a parent directory", err)
			}
		}
		next, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(dirfd)
		if err != nil {
			return -1, pathError(refused, dir, "a parent directory", err)
		}
		dirfd = next
	}
	return dirfd, nil
}

// createBeneath opens rel, a clean relative path, for writing below the
// directory root. Missing parent directories are created with dirMode. No
// component of rel is followed if it is a symbolic link, so the file
// opened is inside root whatever the command left there, and it must be a
// regular file, which is truncated.
func createBeneath(root, rel string, dirMode, fileMode os.FileMode) (*os.File, error) {
	parts, err := splitBeneath(rel)
	if err != nil {
		return nil, err
	}
	dirfd, err := walkBeneath(root, parts[:len(parts)-1], true, dirMode, errCaptureRefused)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dirfd) }()

	// O_NONBLOCK makes a FIFO without a reader fail with ENXIO instead of
	// blocking the open; regularFile then refuses anything that is not a
	// regular file before the truncate.
	fd, err := unix.Openat(dirfd, parts[len(parts)-1],
		unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(fileMode.Perm()))
	if err != nil {
		return nil, pathError(errCaptureRefused, rel, "the destination", err)
	}
	f, err := regularFile(fd, filepath.Join(root, rel), rel, "the destination", errCaptureRefused)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to truncate %s: %w", rel, err)
	}
	return f, nil
}

// openBeneath opens rel, a clean relative path, for reading below the
// directory root without following a symbolic link in any component, and
// requires a regular file. A refusal wraps errWorkspacePathRefused.
func openBeneath(root, rel string) (*os.File, error) {
	parts, err := splitBeneath(rel)
	if err != nil {
		return nil, err
	}
	dirfd, err := walkBeneath(root, parts[:len(parts)-1], false, 0, errWorkspacePathRefused)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dirfd) }()
	fd, err := unix.Openat(dirfd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, pathError(errWorkspacePathRefused, rel, "the file", err)
	}
	return regularFile(fd, filepath.Join(root, rel), rel, "the file", errWorkspacePathRefused)
}

// mkdirBeneath creates rel, a clean relative path, and its missing parents
// below the directory root with mode, without following a symbolic link in
// any component. A refusal wraps errWorkspacePathRefused.
func mkdirBeneath(root, rel string, mode os.FileMode) error {
	parts, err := splitBeneath(rel)
	if err != nil {
		return err
	}
	dirfd, err := walkBeneath(root, parts, true, mode, errWorkspacePathRefused)
	if err != nil {
		return err
	}
	return unix.Close(dirfd)
}

// regularFile wraps fd as a blocking *os.File when it refers to a regular
// file, and closes it otherwise; the refusal wraps refused.
func regularFile(fd int, path, shown, what string, refused error) (*os.File, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to stat %s: %w", shown, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: %s (%s) is not a regular file", refused, shown, what)
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to set %s blocking: %w", shown, err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// pathError reports a failed open or mkdir on a path a command can shape.
// The errors the command can cause that way are wrapped in refused; any
// other error, such as a full disk, is returned as the agent's own.
func pathError(refused error, shown, what string, err error) error {
	var reason string
	switch {
	case errors.Is(err, unix.ELOOP):
		reason = "is a symbolic link"
	case errors.Is(err, unix.ENOTDIR):
		reason = "is not a directory"
	case errors.Is(err, unix.EISDIR):
		reason = "is a directory"
	case errors.Is(err, unix.ENXIO):
		reason = "is not a regular file"
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM), errors.Is(err, unix.ETXTBSY):
		reason = "is not writable"
	default:
		return fmt.Errorf("failed to open %s %s: %w", what, shown, err)
	}
	return fmt.Errorf("%w: %s (%s) %s", refused, shown, what, reason)
}

// mkdirWorkdir creates workdir, an absolute path that securePathUnder
// resolved under root, with mode 0755 and without following a symbolic
// link below root.
func mkdirWorkdir(root, workdir string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace root %s: %w", root, err)
	}
	rel, err := filepath.Rel(absRoot, workdir)
	if err != nil {
		return err
	}
	if rel == "." {
		return os.MkdirAll(absRoot, 0o755)
	}
	return mkdirBeneath(absRoot, rel, 0o755)
}

// stageStdinPath resolves a spec's stdin path and returns the path the
// child opens. A path inside the workspace, relative or absolute, is
// opened beneath the root without following a symbolic link and copied to
// staged, in the agent's staging session, which the command cannot reach;
// the child and the stdin upload then read the copy. Commands of the run
// can write the workspace, and both the agent and the child open stdin
// before Landlock applies, so following a link there would hand the command
// a file only the agent can read. An absolute path outside the workspace,
// such as /dev/null, is chosen by the pipeline and used as given.
func stageStdinPath(root, p, staged string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root %s: %w", root, err)
	}
	rel := p
	if filepath.IsAbs(p) {
		r, err := filepath.Rel(absRoot, filepath.Clean(p))
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return p, nil
		}
		rel = r
	}
	src, err := securePathUnder(absRoot, absRoot, rel)
	if err != nil {
		return "", err
	}
	if src == absRoot {
		return "", fmt.Errorf("path %q is the workspace root", p)
	}
	below, err := filepath.Rel(absRoot, src)
	if err != nil {
		return "", err
	}
	in, err := openBeneath(absRoot, below)
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("failed to stage stdin: %w", err)
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("failed to stage stdin: %w", err)
	}
	return staged, nil
}
