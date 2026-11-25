package git

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

// ConfigEntry is a repository-local key/value pair, including duplicates.
type ConfigEntry struct{ Key, Value string }

// ListConfig returns local config entries without following includes.
func (r *Repository) ListConfig(ctx context.Context) ([]ConfigEntry, error) {
	s, e := r.out(ctx, "config", "--local", "--no-includes", "-z", "--list")
	if e != nil {
		return nil, e
	}
	rows, e := records(s)
	if e != nil {
		return nil, e
	}
	out := []ConfigEntry{}
	for _, v := range rows {
		k, val, _ := strings.Cut(v, "\n")
		out = append(out, ConfigEntry{k, val})
	}
	return out, nil
}

// GetConfig returns the last local value and whether the key exists.
func (r *Repository) GetConfig(ctx context.Context, key string) (string, bool, error) {
	if !operand(key) {
		return "", false, ErrInvalidArgument
	}
	// With a command-scope default, Git reports success for an absent key
	// and still validates its syntax. A stored empty value has local scope.
	output, err := r.out(ctx, "config", "--local", "--no-includes", "--show-scope", "-z", "--default", "", "--get", "--", key)
	if status(err, 1) {
		return "", false, errors.Join(ErrInvalidArgument, err)
	}
	if err != nil {
		return "", false, err
	}
	fields, err := records(output)
	if err != nil || len(fields) != 2 {
		return "", false, errFormat
	}
	switch fields[0] {
	case "local":
		return fields[1], true, nil
	case "command":
		return "", false, nil
	default:
		return "", false, errFormat
	}
}

// SetConfig writes a local setting. Keys and values must be trusted.
func (r *Repository) SetConfig(ctx context.Context, key, value string) error {
	if !operand(key) {
		return ErrInvalidArgument
	}
	return r.do(ctx, "config", "--local", "--", key, value)
}

// UnsetConfig removes all local values for key.
func (r *Repository) UnsetConfig(ctx context.Context, key string) error {
	if !operand(key) {
		return ErrInvalidArgument
	}
	return r.do(ctx, "config", "--local", "--unset-all", "--", key)
}

// Fsck checks object connectivity and integrity.
func (r *Repository) Fsck(ctx context.Context) error {
	return r.client.run(ctx, r.path, io.Discard, "fsck", "--full", "--strict", "--no-dangling", "--no-progress")
}

// GC repacks the repository while retaining unreachable objects.
func (r *Repository) GC(ctx context.Context) error { return r.do(ctx, "gc", "--no-prune") }

// ObjectCounts reports loose and packed storage. Sizes are in KiB.
type ObjectCounts struct{ Loose, Packed, Packs, LooseKiB, PackKiB int64 }

// CountObjects reads Git's storage counters.
func (r *Repository) CountObjects(ctx context.Context) (ObjectCounts, error) {
	s, e := r.out(ctx, "count-objects", "-v")
	var c ObjectCounts
	if e != nil {
		return c, e
	}
	fields := map[string]*int64{"count": &c.Loose, "in-pack": &c.Packed, "packs": &c.Packs, "size": &c.LooseKiB, "size-pack": &c.PackKiB}
	for _, v := range strings.Split(line(s), "\n") {
		k, n, _ := strings.Cut(v, ": ")
		if dst := fields[k]; dst != nil {
			*dst, e = number(n)
			if e != nil {
				return c, errFormat
			}
		}
	}
	return c, nil
}

// Size estimates local object storage in bytes, with KiB granularity.
func (r *Repository) Size(ctx context.Context) (int64, error) {
	c, e := r.CountObjects(ctx)
	return (c.LooseKiB + c.PackKiB) * 1024, e
}

// Archive streams a tar or zip archive of a snapshot; caller must Close.
func (r *Repository) Archive(ctx context.Context, rev, format string) (io.ReadCloser, error) {
	if format != "tar" && format != "zip" {
		return nil, ErrInvalidArgument
	}
	id, e := r.commitID(ctx, rev)
	if e != nil {
		return nil, e
	}
	return r.stream(ctx, "archive", "--format="+format, string(id)), nil
}

// BundleCreate streams all reachable refs and objects. Caller must Close.
func (r *Repository) BundleCreate(ctx context.Context) io.ReadCloser {
	return r.stream(ctx, "bundle", "create", "-", "--all")
}

// BundleVerify checks a trusted bundle path against repository prerequisites.
func (r *Repository) BundleVerify(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidArgument
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	return r.do(ctx, "bundle", "verify", p)
}

// BundleListHeads lists references recorded in a trusted bundle.
func (r *Repository) BundleListHeads(ctx context.Context, path string) ([]Ref, error) {
	if path == "" {
		return nil, ErrInvalidArgument
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	s, e := r.out(ctx, "bundle", "list-heads", p)
	if e != nil {
		return nil, e
	}
	return refLines(s, " ")
}
