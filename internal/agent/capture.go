package agent

import (
	"errors"
	"fmt"
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
// symbolic link and requires a regular file. The staging session is
// reachable by same-user command code, which could otherwise replace its
// capture with a link to a file that the agent can read and the command
// cannot.
func openCapture(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, captureError(filepath.Base(path), "the capture", err)
	}
	return regularFile(fd, path, filepath.Base(path), "the capture")
}

// createBeneath opens rel, a clean relative path, for writing below the
// directory root. Missing parent directories are created with dirMode. No
// component of rel is followed if it is a symbolic link, so the file
// opened is inside root whatever the command left there, and it must be a
// regular file, which is truncated.
func createBeneath(root, rel string, dirMode, fileMode os.FileMode) (*os.File, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			return nil, fmt.Errorf("path %q is not a clean relative path", rel)
		}
	}
	dirfd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open workspace root %s: %w", root, err)
	}
	defer func() { _ = unix.Close(dirfd) }()

	for i, name := range parts[:len(parts)-1] {
		dir := strings.Join(parts[:i+1], "/")
		if err := unix.Mkdirat(dirfd, name, uint32(dirMode.Perm())); err != nil && !errors.Is(err, unix.EEXIST) {
			return nil, captureError(dir, "a parent directory", err)
		}
		next, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, captureError(dir, "a parent directory", err)
		}
		_ = unix.Close(dirfd)
		dirfd = next
	}

	// O_NONBLOCK makes a FIFO without a reader fail with ENXIO instead of
	// blocking the open; regularFile then refuses anything that is not a
	// regular file before the truncate.
	fd, err := unix.Openat(dirfd, parts[len(parts)-1],
		unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(fileMode.Perm()))
	if err != nil {
		return nil, captureError(rel, "the destination", err)
	}
	f, err := regularFile(fd, filepath.Join(root, rel), rel, "the destination")
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to truncate %s: %w", rel, err)
	}
	return f, nil
}

// regularFile wraps fd as a blocking *os.File when it refers to a regular
// file, and closes it otherwise.
func regularFile(fd int, path, shown, what string) (*os.File, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to stat %s: %w", shown, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: %s (%s) is not a regular file", errCaptureRefused, shown, what)
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("failed to set %s blocking: %w", shown, err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

// captureError reports a failed open or mkdir on a capture path. The errors
// a command can cause by shaping the path are wrapped in errCaptureRefused;
// any other error, such as a full disk, is returned as the agent's own.
func captureError(shown, what string, err error) error {
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
	return fmt.Errorf("%w: %s (%s) %s", errCaptureRefused, shown, what, reason)
}
