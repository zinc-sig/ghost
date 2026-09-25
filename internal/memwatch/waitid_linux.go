package memwatch

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// siginfoChild is the prefix of a 64-bit Linux siginfo_t that waitid fills
// for a child: si_signo, si_errno, si_code, padding, then si_pid at offset
// 16. The kernel writes the whole 128 bytes.
type siginfoChild struct {
	signo int32
	errno int32
	code  int32
	_     int32
	pid   int32
	_     [108]byte
}

// peekExited returns the pid of an exited child of this process without
// reaping it, 0 when no child has exited, and -1 on an error such as having
// no children.
func peekExited() int {
	var info siginfoChild
	_, _, errno := unix.Syscall6(unix.SYS_WAITID, unix.P_ALL, 0,
		uintptr(unsafe.Pointer(&info)), unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, 0, 0)
	if errno != 0 {
		return -1
	}
	return int(info.pid)
}
