# Agent package

Package `agent` is ghost's agent mode: a Temporal worker inside a grading
container that serves the `ghost-fetch-submission` and `ghost-run-exec`
activities of the contract in `contract/`. Each exec runs in a child
`ghost exec` process that applies Landlock, the process limit, the per-file
output cap, and the out-of-memory victim mark before `execve`; the agent
itself is never sandboxed. The command reference is the agent section of
`../../USAGE.md`.

## Workspace staging

`ghost-fetch-submission` builds the run workspace from the fetch input in a
fixed order, and every attempt starts that order from the beginning, so a
retried fetch never depends on what an earlier attempt left in the staging
directory:

1. Every `downloads` entry, in list order: the objects under the prefix are
   mirrored into the target directory. Each mirrored file replaces whatever
   is at its path, a directory included, and a file where one of its parent
   directories must be, so the last key listed wins a path and a retried
   attempt rewrites the shape an earlier one left behind.
2. The answer is set aside. When the input carries `answer`, the agent
   lists the regular files directly under the workspace root whose name
   minus its extension equals `stem`, among the root files this attempt's
   delivery downloads wrote (the downloads whose `target_dir` is the root),
   and moves the first in byte-wise name order into a fresh staging
   session. A teacher file an earlier failed attempt left at the root is
   never a candidate, even when its name matches the stem. No match leaves the answer step out
   entirely, so a teacher stub at the target stays. Several matches are a
   delivery shape only the student can produce, so the others stay at the
   root and the agent logs their names.
3. Every `objects` entry, in list order: the object is fetched into the
   staging session, then written to each of its `target_paths` under the
   workspace root (directories 0755, files 0644). A later write replaces an earlier one at the same path, so a
   teacher file overwrites a delivered file with the same name. A
   delivered file where a parent directory must be, or a delivered
   directory at the target, is removed first.
4. The answer is placed at `target_path`, or back under its delivered root
   name when `target_path` is empty, and wins its slot: a teacher object at
   the same path is replaced, and a delivered file or directory in the way
   is removed.

Teacher files are protected: a write that would destroy one is a failure
no rerun can fix. That covers the answer or an object landing on a
directory that holds a teacher file (an object's, or one mirrored from a
mount), or needing a directory where a teacher file sits; one object whose
targets nest; and an object key that does not exist. The activity then
fails with the non-retryable `GhostStagingInvalid` error, before the
offending write, and core reports a configuration failure of the marking
scheme. Replacing one teacher file at its exact path is not a failure: a
later object wins, and the answer always wins its target. Nothing in the
student's delivery raises the error.

The set-aside and the placement cross filesystems when the staging
directory is a tmpfs and the workspace a volume, so both fall back from a
rename to a copy. A missing object key fails the activity before any of its
targets is prepared; a target that escapes the workspace or names the root
fails it before anything is written. The result counts every written target
as one file.

## Memory budgets and out-of-memory attribution

An exec's `memory_limit_bytes` is a budget on the anonymous and shared
resident memory of its process group. Core sends the effective budget on
every exec; 0 means no per-exec enforcement, and the agent applies no
default of its own, because core sizes the container's memory cap from the
budgets it sent and an agent default could kill an exec the container had
room for. The container cap is the backstop for everything the budget does
not catch. Three layers keep a student memory burst from taking the agent
down with it.

1. **The sampler.** One goroutine, shared by every running exec, reads the
   status of every process under `/proc` every 100 ms and sums `RssAnon`
   plus `RssShmem` over each registered exec's members: the exec's root
   process, every descendant of it by parent pid, and every process in its
   group (`NSpgid`; the child is started with `Setpgid`, so its group id is
   its pid). When a sum passes the budget the sampler confirms and then
   sends `SIGKILL` to the group and to every member and flags the exec
   `memory_limit_exceeded`. The largest sum seen is
   reported as `peak_memory_bytes`. The headroom between the summed budgets
   and the cap is the window in which this fires before the kernel does;
   under a fast burst the kernel path is the common one, so the sampler is
   best effort and attribution has to be sound on its own.
2. **Victim ordering.** The kernel picks its victim by badness, which is
   resident size plus `oom_score_adj` scaled to the cgroup's size. `ghost
   exec --oom-victim` writes 1000 to its own `oom_score_adj` before
   `execve`, and the value is inherited by every descendant, so a process in
   the student tree outranks the agent at 0 regardless of which process
   made the allocation. Group kill is off in the container cgroup, so one
   process dies.
3. **Landlock blocks the reset.** An unprivileged process may lower its own
   `oom_score_adj` back to the value it started with. The write happens
   before Landlock is applied, and Landlock refuses every write into
   `/proc` afterwards, so the value sticks. Without Landlock only resident
   size ordering protects the agent, and the same holds when the write
   itself fails: `ghost exec` reports the failure on the agent's stderr and
   runs the command anyway, because an unwritable `/proc` is a degraded
   host posture and not a fault of the command.

If the agent dies anyway, the container exits, the activity heartbeat
lapses, and core fails the run as an infrastructure failure.

### Attribution of a kernel kill

The sampler reads the cgroup's `oom_kill` counter from
`/sys/fs/cgroup/memory.events` on every tick. When it rises between two
ticks, the candidate is the exec that lost a member in that tick. The
candidate is flagged `memory_limit_exceeded`, and its members are killed, when its last sampled sum was at or over its budget, or when it was
the only registered exec at that tick. Otherwise no flag is set and the exit
code stands, because the kernel may have chosen that process for memory
another exec or the agent allocated. When the counter is unreadable, as on
a cgroup v1 host, no kill is attributed; when `/proc` is unreadable nothing
is enforced and the peak is 0.

The sampler charges only what `/proc/<pid>/status` shows as resident
anonymous and shared memory. It cannot see:

- memfd and tmpfs pages that no process has mapped, including files under
  `/dev/shm` after the writer unmapped or closed them;
- page cache and dirty file pages;
- kernel memory such as socket buffers and page tables;
- the agent's own heap.

All of these count against the container cap, which is why a kill by the
kernel can arrive while every sampled sum is under budget.

### Copy-on-write overcount and the confirmation

After a fork, `RssAnon` counts the shared copy-on-write pages in both the
parent and the child, so a harness that forks a large process reads as
twice its size. When a sum passes the budget the sampler therefore sums
`Pss_Anon` plus `Pss_Shmem` from each member's `smaps_rollup`, which
divides shared pages among their sharers, and kills only when that sum is
also over budget. The rollup is a page-table walk, so after a confirmation
finds the group under budget the sampler skips confirmation for that exec
for its next ten over-budget ticks; a fork-heavy harness does not pay the
walk on every tick. A member whose `smaps_rollup` cannot be read but whose status can is
charged its resident sum, so a kernel without the file still enforces the
budget.

### Escaping the group and the sweep

The seccomp allowlist permits `setsid` and `setpgid`, so a student process
can leave its exec's group. Leaving the group does not leave the budget: a
member is any descendant of the exec's root, so a child that called
`setsid` while its parent chain to the root lives is still charged and
killed with the exec. The one process the sampler cannot charge is an
orphan in a group of its own: a descendant whose parent exited, which
reparents to the agent and so is no longer a descendant of the root, and
that also left the root's group. A double fork (the child forks the real
worker into a new session and exits at once) produces exactly that, and
such a worker is bounded only by the container cap while it runs. It is
never charged to an exec by guesswork, because with concurrent execs the
agent cannot tell whose it is. After each exec ends and its members are
killed, the agent enumerates `/proc` and sends `SIGKILL` to every live
descendant of the agent that is not a member of a still-running exec, which
ends such a worker before the next stage. Descendants are found
by walking parent pids: an escaped process reparents to the agent when its
parent exits, because the agent is pid 1 in the container and sets itself
as a child subreaper elsewhere. A child is started and registered under the
sampler's lock so a sibling's sweep cannot kill it before it is registered.
The pids killed are logged. When the agent is not pid 1 a swept process
stays a zombie of the agent, because only pid 1 runs the reaper.

`ghost supervise` closes the gap for the one exec it serves: with a memory
budget it makes itself a child subreaper, so an orphan is reparented to it
rather than to the container's init, and it charges every descendant of
itself to that exec. It reaps the orphans that exit on `SIGCHLD`, because a
zombie counts against the task cap until it is reaped.

### The agent's own memory

The agent shares the container cap with the student processes. Core sizes
the cap with 256 MiB of headroom for the agent, made of a 192 MiB Go soft
memory limit (`GOMEMLIMIT` overrides it) and the 64 MiB shared-memory
tmpfs. Inside the Go limit sit the agent's baseline heap and one 16 MiB
upload buffer per concurrent exec: uploads run with one part in flight and
the part size pinned to 16 MiB, because an upload of unknown size would
otherwise allocate a part buffer sized for the largest object the store
accepts.
