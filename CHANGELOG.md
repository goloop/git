# Changelog

## v0.1.0 (unreleased)

Streaming reads now have their own concurrency limit. Commands use the
caller's deadline unless a client timeout is set. SSH operations honor
the configured SSH command.

Report merge conflicts, missing refs and rejected pushes through typed errors.
Preserve annotated tag messages and handle invalid timezone offsets in imported
history. Managed repositories support reftable. Smart HTTP can record the
authenticated account in push reflogs.

## v0.0.1 (development snapshot)

Initial native Git client for worktrees and bare repositories. Includes
snapshot and history inspection, ref and index updates, synchronization,
maintenance, archive and bundle streams, and authorized smart HTTP hosting.
