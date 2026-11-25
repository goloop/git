# Operations and capabilities

All operations that start Git take a context. Client and repository handles
support concurrent readers. Mutations use Git locks; two writers may race and
one can fail. Resolve once and pass the returned full ID to subsequent reads
to traverse a single snapshot. Separate calls using a branch name may observe
different commits. A series of filesystem, config, or network calls is not a
transaction.

| API | Effect | Bare repositories |
| --- | --- | --- |
| New, Version, Capabilities | Locate and inspect native Git | Independent |
| Open, OpenManaged, Info, Head, IsBare, IsShallow | Inspect layout and HEAD | Yes |
| Init, Clone | Create storage; clone accesses a transport | Yes |
| Resolve, Refs, SymbolicRef, Branches, Tags, TagInfo | Read names and direct targets | Yes |
| UpdateRef, CreateBranch, DeleteBranch, RenameBranch, CreateTag, DeleteTag | Change refs | Yes, subject to native branch checks |
| Log, CommitInfo, Parents, Tree, ObjectInfo, Blob, ReadFile, Blame | Read snapshots and attribution | Yes |
| Diff, DiffStat, ChangedFiles | Read patches, line counts or file status | Commit-to-commit |
| Status, Add, Remove, Restore, Checkout, Switch, Commit | Inspect status or change worktree, index and history | Worktree required |
| MergeBase, IsAncestor, MergeTree | Read ancestry; MergeTree writes candidate objects | Yes |
| Merge, Pull | Change current branch and checkout; Pull fetches first | Worktree required |
| Remotes, RemoteInfo, AddRemote, RemoveRemote | Read or change remote configuration and tracking refs | Yes |
| Fetch, Push, LsRemote | Explicit transport access | Yes |
| GetConfig, SetConfig, UnsetConfig, ListConfig | Read or change local configuration | Yes |
| Fsck, CountObjects, Size, GC | Verify, measure or repack object storage | Yes |
| Archive, BundleCreate, BundleVerify, BundleListHeads | Export a snapshot or reachable history; inspect a bundle | Yes |

## Git baseline and formats

Go 1.24 and Git 2.43 are the minimums. Capabilities caches a successful check of the native release
baseline for SHA-256 and two-commit merge-tree support. Runtime errors from
vendor builds, missing helpers or unsupported filesystems propagate to callers.
SHA-1 and SHA-256 IDs remain full hexadecimal strings. A clone requires
compatible object formats at the transport endpoints.

Log defaults to 100 entries ending at HEAD. Since selects the excluded side
of a two-dot range. Commit timestamps retain valid recorded offsets. An invalid offset in imported
history yields the original instant in UTC. Paths are
literal slash-separated names. Tree and change results preserve raw bytes in
Go strings, including tabs, newlines and non-UTF-8 bytes. JSON consumers must
encode raw path bytes explicitly to preserve invalid UTF-8. ReadFile returns
the bytes stored in a symlink blob. Checkout detaches HEAD; Switch selects a
local branch. Restore replaces unstaged content. UpdateRef requires an
expected target; an empty old ID requests creation only. Never retry a failed
compare-and-swap with a newly observed ID without revisiting the decision.

## Resource use and errors

By default a client has 16 ordinary command slots, 16 streaming slots and
a 16 MiB capture limit per command. StreamConcurrency defaults to Concurrency.
A stream left unread for five minutes returns ErrStreamIdle and releases its
resources. StreamIdleTimeout sets the interval, and a negative value disables
it. The interval starts at open and restarts after a successful read. A caller
waiting inside Read can wait until the operation completes or its context ends. A caller can inspect metadata while consuming it, even with one slot
in each pool. Set a deadline on the context to bound the entire operation.
ClientOptions.Timeout optionally caps each command, including queue time and
stream delivery. Zero uses the caller's context alone. Structured calls fail with
ErrOutputLimit on overflow. Blob, Diff, Blame, Archive and BundleCreate stream
without that capture limit. Always close streams, even after reading to EOF.
An error while reading invalidates partial output. Stage backup files under a
temporary name and publish them only after a successful copy.

Blank repository paths, option-like revision operands and traversal paths
are rejected. A missing HEAD on an empty branch returns its symbolic name
with ErrUnbornHEAD. A detached HEAD returns a commit ID and empty branch.
Merge reports ErrConflict when the index contains unmerged entries after a
failed merge. A missing branch or tag on deletion yields ErrRefNotFound.
RemoteInfo reports ErrRemoteNotFound for a missing remote. Push reports
ErrPushRejected when Git identifies a rejected ref update. These errors
also retain the CommandError for inspection. Other transport failures expose
the Git operation and exit status; child diagnostics are
discarded because they can contain credentials or object data.

## Managed repositories

OpenManaged permits offline metadata, refs, ancestry, commit/tree/blob,
blame, diff and object-count reads. Mutations, external transports and
general config access require the trusted repository API. OpenManaged and
githttp accept administrator-created bare repositories under a canonical
absolute root. IDs consist of slash-separated ASCII names ending in .git.
The restricted config grammar accepts the basic bare layout and object
format emitted by git init, including files and reftable refs on supporting
Git versions. Run the service as the repository owner. Includes, alternate stores, linked worktrees,
partial-clone configs, helper settings and unknown extensions are rejected.
Keep metadata and directory ownership trusted throughout each handle's
lifetime. Filesystem checks alone cannot prevent concurrent replacement.

githttp authorizes the URL identifier before invoking its resolver. Keep its
mapping stable for the authorization decision. Callbacks must respect the
request context. The default is read-only. Writes require EnablePush and
write authorization on discovery and RPC. Authenticate in middleware before
the handler. Return HTTP 401 with WWW-Authenticate when credentials are
needed. A denied authorization returns 403. The managed backend rejects ref
deletions and non-fast-forward branch updates and disables repository hooks.
To attribute pushes, enable core.logallrefupdates during provisioning and
supply RemoteUser from the authenticated account. The fallback identity is
"authenticated". Serve behind authenticated TLS.
HTTP limits default to eight concurrent requests, five minutes, and 1 GiB
in each direction. Increase the handler Timeout for slow, large clones and
align server and proxy deadlines. Oversized declared bodies return 413. A response failure
after headers aborts the HTTP stream. Configure socket deadlines at the
server or proxy. A packfile can require CPU, memory and disk far beyond its
network size. Enforce quotas outside the process.

The trusted client scrubs inherited GIT_* variables, disables system config
and uses the local user's global and repository configuration. Prompts are
disabled for Git by default. SSH uses the trusted core.sshCommand or the
host SSH configuration. Configure BatchMode for unattended SSH clients.
Pull fast-forwards the current checkout from the requested remote branch.
The managed environment also removes global config, disables hooks, external
transports, lazy fetches and automatic maintenance. Worker isolation remains
necessary when processing hostile objects.

A bundle carries advertised refs and their reachable history. Back up config,
hooks, reflogs, unreachable objects, LFS storage and hosting metadata separately.
Size estimates object storage using Git's KiB counters. GC runs explicitly
with --no-prune; native reflog and repacking policies still apply.
