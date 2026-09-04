// Package sandbox provides the Linux isolation primitives ghost applies to a
// child command: Landlock filesystem restrictions, a pure-Go seccomp-BPF
// filter installer, and cgroup v2 memory and OOM accounting. On other
// platforms every operation is a no-op.
package sandbox
