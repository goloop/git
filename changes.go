package git

import (
	"context"
	"io"
	"strings"
)

// DiffOptions selects unstaged (zero), staged, or commit comparisons.
// From alone compares a commit to the worktree or index. To requires From.
type DiffOptions struct {
	From, To                            string
	Staged, DetectRenames, DetectCopies bool
	Paths                               []string
}

func (r *Repository) diffArgs(ctx context.Context, o DiffOptions, format ...string) ([]string, error) {
	if o.To != "" && (o.From == "" || o.Staged) {
		return nil, ErrInvalidArgument
	}
	if o.To == "" && r.bare {
		return nil, ErrBareRepository
	}
	a := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--ignore-submodules=dirty"}
	a = append(a, format...)
	if o.DetectCopies {
		a = append(a, "-C", "--find-copies-harder")
	} else if o.DetectRenames {
		a = append(a, "-M")
	} else {
		a = append(a, "--no-renames")
	}
	if o.Staged {
		a = append(a, "--cached")
	}
	for _, rev := range []string{o.From, o.To} {
		if rev != "" {
			id, e := r.commitID(ctx, rev)
			if e != nil {
				return nil, e
			}
			a = append(a, string(id))
		}
	}
	for _, p := range o.Paths {
		if !validPath(p) {
			return nil, ErrInvalidArgument
		}
	}
	return append(append(a, "--"), o.Paths...), nil
}

// Diff streams a raw patch. Close the stream even after a read failure.
func (r *Repository) Diff(ctx context.Context, o DiffOptions) (io.ReadCloser, error) {
	a, e := r.diffArgs(ctx, o, "--patch")
	if e != nil {
		return nil, e
	}
	return r.stream(ctx, a...), nil
}

// FileChange describes an addition, deletion, modification, copy or rename.
type FileChange struct{ Status, Path, OriginalPath string }

// ChangedFiles reads NUL-delimited name/status records.
func (r *Repository) ChangedFiles(ctx context.Context, o DiffOptions) ([]FileChange, error) {
	a, e := r.diffArgs(ctx, o, "--name-status", "-z")
	if e != nil {
		return nil, e
	}
	s, e := r.out(ctx, a...)
	if e != nil {
		return nil, e
	}
	f, e := records(s)
	if e != nil {
		return nil, e
	}
	out := []FileChange{}
	for len(f) > 0 {
		if len(f) < 2 || f[0] == "" {
			return nil, errFormat
		}
		v := FileChange{Status: f[0], Path: f[1]}
		f = f[2:]
		if v.Status[0] == 'R' || v.Status[0] == 'C' {
			if len(f) == 0 {
				return nil, errFormat
			}
			v.OriginalPath, v.Path = v.Path, f[0]
			f = f[1:]
		}
		out = append(out, v)
	}
	return out, nil
}

// FileStat gives line counts or Binary for non-textual changes.
type FileStat struct {
	Path, OriginalPath string
	Added, Deleted     int64
	Binary             bool
}

// DiffStat reads per-file statistics including both rename paths.
func (r *Repository) DiffStat(ctx context.Context, o DiffOptions) ([]FileStat, error) {
	a, e := r.diffArgs(ctx, o, "--numstat", "-z")
	if e != nil {
		return nil, e
	}
	s, e := r.out(ctx, a...)
	if e != nil {
		return nil, e
	}
	f, e := records(s)
	if e != nil {
		return nil, e
	}
	out := []FileStat{}
	for len(f) > 0 {
		v := strings.SplitN(f[0], "\t", 3)
		f = f[1:]
		if len(v) != 3 {
			return nil, errFormat
		}
		item := FileStat{Path: v[2], Binary: v[0] == "-" && v[1] == "-"}
		if !item.Binary {
			item.Added, e = number(v[0])
			if e != nil {
				return nil, errFormat
			}
			item.Deleted, e = number(v[1])
			if e != nil {
				return nil, errFormat
			}
		}
		if item.Path == "" {
			if len(f) < 2 {
				return nil, errFormat
			}
			item.OriginalPath, item.Path = f[0], f[1]
			f = f[2:]
		}
		out = append(out, item)
	}
	return out, nil
}

// StatusEntry preserves porcelain v2 record kind and index/worktree status.
type StatusEntry struct{ Kind, XY, Path, OriginalPath string }

// Status inspects tracked, conflicted and untracked paths.
func (r *Repository) Status(ctx context.Context) ([]StatusEntry, error) {
	if r.bare {
		return nil, ErrBareRepository
	}
	s, e := r.out(ctx, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=dirty")
	if e != nil {
		return nil, e
	}
	return parseStatus(s)
}
func parseStatus(s string) ([]StatusEntry, error) {
	f, e := records(s)
	if e != nil {
		return nil, e
	}
	out := []StatusEntry{}
	for len(f) > 0 {
		row := f[0]
		f = f[1:]
		if len(row) < 3 {
			return nil, errFormat
		}
		kind := row[:1]
		count := 0
		switch kind {
		case "1":
			count = 9
		case "2":
			count = 10
		case "u":
			count = 11
		case "?", "!":
			count = 2
		default:
			return nil, errFormat
		}
		v := strings.SplitN(row, " ", count)
		if len(v) != count || v[0] != kind || v[count-1] == "" {
			return nil, errFormat
		}
		item := StatusEntry{Kind: kind, Path: v[count-1], XY: kind}
		if count > 2 {
			if len(v[1]) != 2 {
				return nil, errFormat
			}
			item.XY = v[1]
		}
		if kind == "2" {
			if len(f) == 0 {
				return nil, errFormat
			}
			item.OriginalPath = f[0]
			f = f[1:]
		}
		out = append(out, item)
	}
	return out, nil
}

// Blame streams Git's line-porcelain attribution for a literal file at rev.
func (r *Repository) Blame(ctx context.Context, rev, path string) (io.ReadCloser, error) {
	if !validPath(path) {
		return nil, ErrInvalidArgument
	}
	id, e := r.commitID(ctx, rev)
	if e != nil {
		return nil, e
	}
	return r.stream(ctx, "blame", "--line-porcelain", "--no-textconv", string(id), "--", path), nil
}
