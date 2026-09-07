package agent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// procSample is one process as read from its status file under /proc: its
// identity, its process group in the agent's pid namespace, and the
// resident memory the sampler charges to that group.
type procSample struct {
	pid    int
	ppid   int
	pgid   int
	zombie bool
	// rssAnon and rssShmem are bytes. A zombie has neither line and
	// reports 0.
	rssAnon  int64
	rssShmem int64
}

// groupSample is the memory the sampler charges to one process group: the
// sum of RssAnon and RssShmem over its members, and the member pids.
type groupSample struct {
	sum  int64
	pids []int
}

// parseProcStatus parses the fields of one /proc/<pid>/status file that the
// sampler uses. NSpgid lists the group id once per nested pid namespace and
// the first value is the one in the namespace that mounted /proc, which is
// the agent's; a status file without NSpgid cannot be grouped and is an
// error. Missing RssAnon and RssShmem lines read as 0.
func parseProcStatus(data []byte) (procSample, error) {
	var s procSample
	havePid, havePgid := false, false
	for _, line := range bytes.Split(data, []byte("\n")) {
		key, value, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			continue
		}
		fields := strings.Fields(string(value))
		if len(fields) == 0 {
			continue
		}
		switch string(key) {
		case "Pid":
			n, err := strconv.Atoi(fields[0])
			if err != nil {
				return s, err
			}
			s.pid, havePid = n, true
		case "PPid":
			n, err := strconv.Atoi(fields[0])
			if err != nil {
				return s, err
			}
			s.ppid = n
		case "NSpgid":
			n, err := strconv.Atoi(fields[0])
			if err != nil {
				return s, err
			}
			s.pgid, havePgid = n, true
		case "State":
			s.zombie = fields[0] == "Z"
		case "RssAnon":
			s.rssAnon = parseKB(fields)
		case "RssShmem":
			s.rssShmem = parseKB(fields)
		}
	}
	if !havePid || !havePgid {
		return s, errors.New("status file lacks Pid or NSpgid")
	}
	return s, nil
}

// parseKB converts a status value of the form "1234 kB" to bytes; anything
// else reads as 0.
func parseKB(fields []string) int64 {
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return n * 1024
}

// snapshotProcs reads the status of every process listed under procRoot.
// A process that vanishes between the directory listing and the read is
// skipped, as is one whose status cannot be parsed. The error is non-nil
// only when the directory itself cannot be listed.
func snapshotProcs(procRoot string) ([]procSample, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	samples := make([]procSample, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "status"))
		if err != nil {
			continue
		}
		s, err := parseProcStatus(data)
		if err != nil {
			continue
		}
		samples = append(samples, s)
	}
	return samples, nil
}

// groupByPgid sums the sampled memory of every process group. Zombies hold
// no memory and are left out of the member lists.
func groupByPgid(samples []procSample) map[int]groupSample {
	groups := make(map[int]groupSample)
	for _, s := range samples {
		if s.zombie {
			continue
		}
		g := groups[s.pgid]
		g.sum += s.rssAnon + s.rssShmem
		g.pids = append(g.pids, s.pid)
		groups[s.pgid] = g
	}
	return groups
}

// readPssBytes returns the proportional anonymous and shared memory of one
// process from its smaps_rollup: Pss_Anon plus Pss_Shmem, in bytes. The
// bool is false when the file cannot be read.
func readPssBytes(procRoot string, pid int) (int64, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "smaps_rollup"))
	if err != nil {
		return 0, false
	}
	return parsePssRollup(data), true
}

// parsePssRollup sums Pss_Anon and Pss_Shmem from smaps_rollup content.
func parsePssRollup(data []byte) int64 {
	var total int64
	for _, line := range bytes.Split(data, []byte("\n")) {
		key, value, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			continue
		}
		if k := string(key); k == "Pss_Anon" || k == "Pss_Shmem" {
			total += parseKB(strings.Fields(string(value)))
		}
	}
	return total
}

// confirmedGroupBytes returns the proportional memory of a process group,
// the sum that discounts pages shared copy-on-write between members. A
// member whose smaps_rollup is unreadable but whose status is still
// readable contributes its resident sum, so a kernel without smaps_rollup
// still enforces the budget; a member that has exited contributes 0.
func confirmedGroupBytes(procRoot string, pids []int) int64 {
	var total int64
	for _, pid := range pids {
		if pss, ok := readPssBytes(procRoot, pid); ok {
			total += pss
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
		if err != nil {
			continue
		}
		if s, err := parseProcStatus(data); err == nil {
			total += s.rssAnon + s.rssShmem
		}
	}
	return total
}
