package git

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// Signature records a name, email and timestamp including its offset.
type Signature struct {
	Name, Email string
	When        time.Time
}

// Commit contains commit metadata and message bytes represented as a string.
type Commit struct {
	ID, Tree          ObjectID
	Parents           []ObjectID
	Author, Committer Signature
	Subject, Message  string
}

// LogOptions bounds history (default 100 commits). Since excludes ancestors
// of the given commit. Topological selects topological traversal.
type LogOptions struct {
	Rev, Since  string
	Limit       int
	Paths       []string
	Topological bool
}

const commitFormat = "%H%x00%T%x00%P%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%s%x00%B"

func parseCommits(s string) ([]Commit, error) {
	fields, e := records(s)
	if e != nil || len(fields)%11 != 0 {
		return nil, errFormat
	}
	out := []Commit{}
	for len(fields) > 0 {
		f := fields[:11]
		fields = fields[11:]
		a, e := time.Parse(time.RFC3339, f[5])
		if e != nil {
			return nil, errFormat
		}
		c, e := time.Parse(time.RFC3339, f[8])
		if e != nil {
			return nil, errFormat
		}
		v := Commit{ID: ObjectID(f[0]), Tree: ObjectID(f[1]), Parents: []ObjectID{}, Author: Signature{f[3], f[4], a}, Committer: Signature{f[6], f[7], c}, Subject: f[9], Message: f[10]}
		for _, p := range strings.Fields(f[2]) {
			v.Parents = append(v.Parents, ObjectID(p))
		}
		out = append(out, v)
	}
	return out, nil
}

// Log reads bounded history, optionally restricted to literal paths.
func (r *Repository) Log(ctx context.Context, o LogOptions) ([]Commit, error) {
	if o.Limit < 0 {
		return nil, ErrInvalidArgument
	}
	if o.Limit == 0 {
		o.Limit = 100
	}
	if o.Rev == "" {
		o.Rev = "HEAD"
	}
	end, e := r.commitID(ctx, o.Rev)
	if e != nil {
		return nil, e
	}
	a := []string{"log", "--no-notes", "--no-show-signature", "--encoding=none", "-z", "--format=" + commitFormat, "-n", itoa(o.Limit)}
	if o.Topological {
		a = append(a, "--topo-order")
	}
	if o.Since != "" {
		base, e := r.commitID(ctx, o.Since)
		if e != nil {
			return nil, e
		}
		a = append(a, "^"+string(base))
	}
	a = append(a, string(end), "--")
	for _, p := range o.Paths {
		if !validPath(p) {
			return nil, ErrInvalidArgument
		}
	}
	s, e := r.out(ctx, append(a, o.Paths...)...)
	if e != nil {
		return nil, e
	}
	return parseCommits(s)
}

// CommitInfo reads one commit by revision.
func (r *Repository) CommitInfo(ctx context.Context, rev string) (Commit, error) {
	v, e := r.Log(ctx, LogOptions{Rev: rev, Limit: 1})
	if e != nil {
		return Commit{}, e
	}
	if len(v) != 1 {
		return Commit{}, errFormat
	}
	return v[0], nil
}

// Parents returns ordered immediate parent IDs.
func (r *Repository) Parents(ctx context.Context, rev string) ([]ObjectID, error) {
	c, e := r.CommitInfo(ctx, rev)
	return c.Parents, e
}

// TreeEntry describes a tree member. Size is -1 for non-blobs.
type TreeEntry struct {
	Path, Mode, Type string
	ID               ObjectID
	Size             int64
}

// Tree lists immediate or recursive entries at rev.
func (r *Repository) Tree(ctx context.Context, rev string, recursive bool) ([]TreeEntry, error) {
	if !operand(rev) {
		return nil, ErrInvalidArgument
	}
	id, e := r.resolve(ctx, rev+"^{tree}")
	if e != nil {
		return nil, e
	}
	a := []string{"ls-tree", "-z", "-l"}
	if recursive {
		a = append(a, "-r")
	}
	s, e := r.out(ctx, append(a, string(id))...)
	if e != nil {
		return nil, e
	}
	rec, e := records(s)
	if e != nil {
		return nil, e
	}
	out := []TreeEntry{}
	for _, v := range rec {
		meta, p, ok := strings.Cut(v, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 4 {
			return nil, errFormat
		}
		size := int64(-1)
		if f[3] != "-" {
			size, e = number(f[3])
			if e != nil {
				return nil, errFormat
			}
		}
		out = append(out, TreeEntry{p, f[0], f[1], ObjectID(f[2]), size})
	}
	return out, nil
}

// ObjectInfo describes a resolved object in bytes.
type ObjectInfo struct {
	ID   ObjectID
	Type string
	Size int64
}

// ObjectInfo reads an object's native type and size.
func (r *Repository) ObjectInfo(ctx context.Context, rev string) (ObjectInfo, error) {
	if !operand(rev) {
		return ObjectInfo{}, ErrInvalidArgument
	}
	s, e := r.client.query(ctx, r.path, strings.NewReader(rev+"\x00"), "cat-file", "-Z", "--batch-check")
	if e != nil {
		return ObjectInfo{}, e
	}
	row, ok := strings.CutSuffix(s, "\x00")
	if !ok {
		return ObjectInfo{}, errFormat
	}
	if row == rev+" missing" {
		return ObjectInfo{}, ErrObjectNotFound
	}
	f := strings.Split(row, " ")
	if len(f) != 3 || !validID(ObjectID(f[0])) {
		return ObjectInfo{}, errFormat
	}
	n, e := number(f[2])
	if e != nil || n < 0 {
		return ObjectInfo{}, errFormat
	}
	return ObjectInfo{ObjectID(f[0]), f[1], n}, nil
}

// Blob opens an object stream. Read to EOF for process errors; always Close.
func (r *Repository) Blob(ctx context.Context, rev string) (io.ReadCloser, error) {
	id, e := r.Resolve(ctx, rev)
	if e != nil {
		return nil, e
	}
	return r.stream(ctx, "cat-file", "blob", string(id)), nil
}

// ReadFile reads up to limit bytes from a tree, preserving stored symlink data.
func (r *Repository) ReadFile(ctx context.Context, rev, path string, limit int64) ([]byte, error) {
	if !validPath(path) || !operand(rev) || limit < 0 {
		return nil, ErrInvalidArgument
	}
	tree, e := r.resolve(ctx, rev+"^{tree}")
	if e != nil {
		return nil, e
	}
	info, e := r.ObjectInfo(ctx, string(tree)+":"+path)
	if e == ErrObjectNotFound {
		return nil, ErrPathNotFound
	}
	if e != nil {
		return nil, e
	}
	if limit > r.client.opts.MaxOutput {
		limit = r.client.opts.MaxOutput
	}
	if info.Type != "blob" {
		return nil, ErrInvalidArgument
	}
	if info.Size > limit {
		return nil, ErrOutputLimit
	}
	b := bounded{max: limit}
	e = r.client.run(ctx, r.path, &writeOnly{&b}, "cat-file", "blob", string(info.ID))
	if b.overflow {
		return nil, ErrOutputLimit
	}
	if e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
