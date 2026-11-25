package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Repository pins the working directory for subsequent operations.
type Repository struct {
	client *Client
	path   string
	bare   bool
}

// RepositoryInfo identifies the checkout, common object store and object format.
type RepositoryInfo struct {
	Path, GitDir, CommonDir, Worktree, ObjectFormat string
	Bare, Shallow                                   bool
}

// InitOptions sets repository layout. Empty names use Git defaults.
type InitOptions struct {
	Bare                        bool
	InitialBranch, ObjectFormat string
}

// CloneOptions requests a bare or shallow clone. Zero depth copies full history.
type CloneOptions struct {
	Bare  bool
	Depth int
}

func (c *Client) prepare(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", ErrInvalidArgument
	}
	if _, e := c.Capabilities(ctx); e != nil {
		return "", e
	}
	return filepath.Abs(path)
}

// Init initializes a repository at path.
func (c *Client) Init(ctx context.Context, path string, o InitOptions) (*Repository, error) {
	p, e := c.prepare(ctx, path)
	if e != nil {
		return nil, e
	}
	a := []string{"init"}
	if o.Bare {
		a = append(a, "--bare")
	}
	if o.InitialBranch != "" {
		a = append(a, "--initial-branch="+o.InitialBranch)
	}
	if o.ObjectFormat != "" {
		if o.ObjectFormat != "sha1" && o.ObjectFormat != "sha256" {
			return nil, ErrInvalidArgument
		}
		a = append(a, "--object-format="+o.ObjectFormat)
	}
	_, e = c.output(ctx, "", append(a, "--", p)...)
	if e != nil {
		return nil, e
	}
	return c.open(ctx, p)
}

// Clone copies a trusted local path or remote URL into a new repository.
func (c *Client) Clone(ctx context.Context, url, path string, o CloneOptions) (*Repository, error) {
	if !operand(url) || o.Depth < 0 {
		return nil, ErrInvalidArgument
	}
	p, e := c.prepare(ctx, path)
	if e != nil {
		return nil, e
	}
	a := []string{"clone", "--no-local", "--no-recurse-submodules"}
	if o.Bare {
		a = append(a, "--bare")
	}
	if o.Depth > 0 {
		a = append(a, "--depth="+itoa(o.Depth))
	}
	_, e = c.output(ctx, "", append(a, "--", url, p)...)
	if e != nil {
		return nil, e
	}
	return c.open(ctx, p)
}

// Open opens an existing repository root, including a linked worktree.
func (c *Client) Open(ctx context.Context, path string) (*Repository, error) {
	p, e := c.prepare(ctx, path)
	if e != nil {
		return nil, e
	}
	return c.open(ctx, p)
}
func (c *Client) open(ctx context.Context, p string) (*Repository, error) {
	p, e := filepath.EvalSymlinks(p)
	if e != nil {
		return nil, e
	}
	if _, err := os.Stat(filepath.Join(p, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err = os.Stat(filepath.Join(p, "HEAD")); errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotRepository
		}
	}
	s, e := c.output(ctx, p, "rev-parse", "--is-bare-repository")
	if e != nil {
		return nil, e
	}
	r := &Repository{c, p, line(s) == "true"}
	flag := "--show-toplevel"
	if r.bare {
		flag = "--absolute-git-dir"
	}
	root, e := r.out(ctx, "rev-parse", flag)
	if e != nil {
		return nil, e
	}
	x, e := os.Stat(p)
	if e != nil {
		return nil, e
	}
	y, e := os.Stat(line(root))
	if e != nil {
		return nil, e
	}
	if !os.SameFile(x, y) {
		return nil, ErrNotRepository
	}
	return r, nil
}
func (r *Repository) out(ctx context.Context, args ...string) (string, error) {
	return r.client.output(ctx, r.path, args...)
}
func (r *Repository) do(ctx context.Context, args ...string) error {
	_, e := r.out(ctx, args...)
	return e
}

// IsBare reports whether the handle has no working tree.
func (r *Repository) IsBare() bool { return r.bare }

// IsShallow queries the current shallow boundary state.
func (r *Repository) IsShallow(ctx context.Context) (bool, error) {
	s, e := r.out(ctx, "rev-parse", "--is-shallow-repository")
	return line(s) == "true", e
}

// Info reads repository layout without interpreting paths as shell text.
func (r *Repository) Info(ctx context.Context) (RepositoryInfo, error) {
	i := RepositoryInfo{Path: r.path, Bare: r.bare}
	if !r.bare {
		i.Worktree = r.path
	}
	for _, field := range []struct {
		dst  *string
		flag string
	}{{&i.GitDir, "--absolute-git-dir"}, {&i.CommonDir, "--git-common-dir"}, {&i.ObjectFormat, "--show-object-format"}} {
		s, e := r.out(ctx, "rev-parse", "--path-format=absolute", field.flag)
		if e != nil {
			return i, e
		}
		*field.dst = line(s)
	}
	var e error
	i.Shallow, e = r.IsShallow(ctx)
	return i, e
}
func validPath(p string) bool {
	if p == "" || strings.IndexByte(p, 0) >= 0 || filepath.Separator == '\\' && strings.ContainsRune(p, '\\') || strings.HasPrefix(p, "/") || filepath.VolumeName(p) != "" {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}
