# Changelog

## v0.2.2 - 2026-11-26

Cover clone, fetch and push through a reverse proxy.

## v0.2.1 - 2026-11-26

Add package badges to both READMEs and publish the contribution guide and code of conduct.

## v0.2.0 (development snapshot)

Unread streams now release their Git process and slot after five minutes.
StreamIdleTimeout controls this interval. A pending Read can still wait for
Git to produce data. On Windows, unattended Git runs without the parent console.

Tags reports each tag's immediate target. Blob checks the resolved type of
annotated tags. GetConfig distinguishes invalid keys, missing keys and empty
values. Clients reuse successful capability checks.

## v0.1.0 (development snapshot)

Streaming reads have their own concurrency limit. The default allows 16
ordinary Git processes plus 16 streaming processes. Commands use the caller's
deadline unless a client timeout is set. SSH honors the configured command.

Report merge conflicts, missing refs and rejected pushes through typed errors.
Preserve annotated tag messages and handle invalid timezone offsets in imported
history. Managed repositories support reftable. Smart HTTP can record the
authenticated account in push reflogs.

## v0.0.1 (development snapshot)

Initial native Git client for worktrees and bare repositories. Includes
snapshot and history inspection, ref and index updates, synchronization,
maintenance, archive and bundle streams, and authorized smart HTTP hosting.
