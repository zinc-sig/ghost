package agent

import (
	"reflect"
	"testing"
)

// TestParseProcStatus_GroupsByFirstNSpgidAndSumsResidentAnonAndShmem
// asserts that the parser takes the first NSpgid value, converts the kB
// fields to bytes, and treats a zombie's missing Rss lines as 0. Taking the
// wrong NSpgid value would charge a nested-namespace process to no exec.
func TestParseProcStatus_GroupsByFirstNSpgidAndSumsResidentAnonAndShmem(t *testing.T) {
	status := []byte("Name:\tpython3\nState:\tS (sleeping)\nPid:\t4242\nPPid:\t4200\n" +
		"NSpid:\t4242\t7\nNSpgid:\t4200\t5\nVmRSS:\t   9000 kB\nRssAnon:\t   6144 kB\n" +
		"RssFile:\t   2000 kB\nRssShmem:\t    1024 kB\n")
	got, err := parseProcStatus(status)
	if err != nil {
		t.Fatalf("parseProcStatus: %v", err)
	}
	want := procSample{pid: 4242, ppid: 4200, pgid: 4200, rssAnon: 6144 * 1024, rssShmem: 1024 * 1024}
	if got != want {
		t.Errorf("parseProcStatus = %+v, want %+v", got, want)
	}

	zombie := []byte("Name:\tsleep\nState:\tZ (zombie)\nPid:\t4300\nPPid:\t1\nNSpgid:\t4300\n")
	got, err = parseProcStatus(zombie)
	if err != nil {
		t.Fatalf("parseProcStatus(zombie): %v", err)
	}
	if !got.zombie || got.rssAnon != 0 || got.rssShmem != 0 {
		t.Errorf("zombie parsed as %+v, want zombie with 0 bytes", got)
	}

	if _, err := parseProcStatus([]byte("Name:\tx\nPid:\t1\n")); err == nil {
		t.Error("a status without NSpgid parsed, want an error so the pid is skipped")
	}
}

// TestGroupByPgid_SumsMembersAndSkipsZombies asserts that a group's sum is
// the RssAnon plus RssShmem of its live members only; charging a zombie
// would keep a pid in the group after it died and mask a kernel kill.
func TestGroupByPgid_SumsMembersAndSkipsZombies(t *testing.T) {
	samples := []procSample{
		{pid: 10, ppid: 1, pgid: 10, rssAnon: 100, rssShmem: 10},
		{pid: 11, ppid: 10, pgid: 10, rssAnon: 200, rssShmem: 20},
		{pid: 12, ppid: 10, pgid: 10, zombie: true},
		{pid: 20, ppid: 1, pgid: 20, rssAnon: 5},
	}
	groups := groupByPgid(samples)
	if g := groups[10]; g.sum != 330 || !reflect.DeepEqual(g.pids, []int{10, 11}) {
		t.Errorf("group 10 = %+v, want sum 330 and pids [10 11]", g)
	}
	if g := groups[20]; g.sum != 5 || !reflect.DeepEqual(g.pids, []int{20}) {
		t.Errorf("group 20 = %+v, want sum 5 and pids [20]", g)
	}
}

// TestParsePssRollup_SumsAnonAndShmemOnly asserts that the confirmation
// value counts Pss_Anon and Pss_Shmem and ignores file-backed pages, which
// the budget does not charge.
func TestParsePssRollup_SumsAnonAndShmemOnly(t *testing.T) {
	rollup := []byte("Rss:\t 9000 kB\nPss:\t 5000 kB\nPss_Anon:\t 3000 kB\nPss_File:\t 1500 kB\nPss_Shmem:\t 500 kB\n")
	if got, want := parsePssRollup(rollup), int64(3500*1024); got != want {
		t.Errorf("parsePssRollup = %d, want %d", got, want)
	}
}

// TestAttributeKernelKill_FlagsOnlyJustifiedCandidates asserts the
// attribution rule: a group that lost a pid while the counter rose is
// flagged when its last sum was at or over its budget or it was the sole
// exec, and is left unflagged otherwise. Flagging every candidate would
// blame an exec for memory a sibling or the agent allocated.
func TestAttributeKernelKill_FlagsOnlyJustifiedCandidates(t *testing.T) {
	cases := []struct {
		name    string
		samples []attributionSample
		want    []int
	}{
		{
			name:    "sole exec under budget is flagged",
			samples: []attributionSample{{lostPid: true, lastSum: 10, budget: 100}},
			want:    []int{0},
		},
		{
			name:    "sole exec without a budget is flagged",
			samples: []attributionSample{{lostPid: true, lastSum: 10, budget: 0}},
			want:    []int{0},
		},
		{
			name:    "sole exec that lost no pid is not flagged",
			samples: []attributionSample{{lostPid: false, lastSum: 500, budget: 100}},
			want:    nil,
		},
		{
			name: "among siblings only the candidate at or over budget is flagged",
			samples: []attributionSample{
				{lostPid: true, lastSum: 100, budget: 100},
				{lostPid: true, lastSum: 50, budget: 100},
				{lostPid: false, lastSum: 900, budget: 100},
			},
			want: []int{0},
		},
		{
			name: "a candidate under budget with a sibling running is not flagged",
			samples: []attributionSample{
				{lostPid: true, lastSum: 50, budget: 100},
				{lostPid: false, lastSum: 50, budget: 100},
			},
			want: nil,
		},
		{
			name: "a candidate without a budget with a sibling running is not flagged",
			samples: []attributionSample{
				{lostPid: true, lastSum: 500, budget: 0},
				{lostPid: false, lastSum: 50, budget: 100},
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attributeKernelKill(tc.samples); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("attributeKernelKill = %v, want %v", got, tc.want)
			}
		})
	}
}
