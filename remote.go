package git

import (
	"context"
	"net/url"
	"strings"
)

// Remote holds a configured name and its redacted fetch URL.
type Remote struct{ Name, URL string }

// Remotes lists configured remote names.
func (r *Repository) Remotes(ctx context.Context) ([]string, error) {
	s, e := r.out(ctx, "remote")
	if e != nil {
		return nil, e
	}
	if s == "" {
		return []string{}, nil
	}
	return strings.Split(line(s), "\n"), nil
}
func remoteName(s string) bool {
	if !operand(s) {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			return false
		}
	}
	return true
}

// AddRemote stores a trusted transport address in repository configuration.
func (r *Repository) AddRemote(ctx context.Context, name, address string) error {
	if !remoteName(name) || !operand(address) {
		return ErrInvalidArgument
	}
	return r.do(ctx, "remote", "add", name, address)
}

// RemoveRemote deletes the remote's configuration and tracking refs.
func (r *Repository) RemoveRemote(ctx context.Context, name string) error {
	if !remoteName(name) {
		return ErrInvalidArgument
	}
	return r.do(ctx, "remote", "remove", name)
}

// RemoteInfo looks up the configured fetch URL, omitting URL userinfo and query.
func (r *Repository) RemoteInfo(ctx context.Context, name string) (Remote, error) {
	if !remoteName(name) {
		return Remote{}, ErrInvalidArgument
	}
	s, e := r.out(ctx, "remote", "get-url", name)
	if e != nil {
		return Remote{}, e
	}
	s = line(s)
	if strings.Contains(s, "://") {
		u, e := url.Parse(s)
		if e != nil {
			return Remote{}, ErrInvalidArgument
		}
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		s = u.String()
	}
	return Remote{name, s}, nil
}

// Fetch downloads a configured remote's refs without recursing into submodules.
func (r *Repository) Fetch(ctx context.Context, name string) error {
	if !remoteName(name) {
		return ErrInvalidArgument
	}
	if _, e := r.RemoteInfo(ctx, name); e != nil {
		return e
	}
	return r.do(ctx, "fetch", "--no-recurse-submodules", "--no-prune", name)
}

// Push publishes a local branch to the same branch on a configured remote.
func (r *Repository) Push(ctx context.Context, name, branch string) error {
	if _, e := r.RemoteInfo(ctx, name); e != nil {
		return e
	}
	if e := r.branchName(ctx, branch); e != nil {
		return e
	}
	ref := "refs/heads/" + branch
	return r.do(ctx, "push", "--no-force", "--no-follow-tags", "--recurse-submodules=no", name, ref+":"+ref)
}

// Pull fetches a named branch and permits only a fast-forward.
func (r *Repository) Pull(ctx context.Context, name, branch string) error {
	if _, e := r.RemoteInfo(ctx, name); e != nil {
		return e
	}
	if e := r.branchName(ctx, branch); e != nil {
		return e
	}
	return r.work(ctx, "pull", "--ff-only", "--no-rebase", "--no-recurse-submodules", name, "refs/heads/"+branch)
}

// LsRemote reads advertised refs over a trusted transport.
func (r *Repository) LsRemote(ctx context.Context, address string) ([]Ref, error) {
	if !operand(address) {
		return nil, ErrInvalidArgument
	}
	s, e := r.out(ctx, "ls-remote", "--refs", "--", address)
	if e != nil {
		return nil, e
	}
	return refLines(s, "\t")
}
func refLines(s, sep string) ([]Ref, error) {
	out := []Ref{}
	if s != "" {
		for _, v := range strings.Split(line(s), "\n") {
			id, n, ok := strings.Cut(v, sep)
			if !ok || !validID(ObjectID(id)) {
				return nil, errFormat
			}
			out = append(out, Ref{Name: n, ID: ObjectID(id)})
		}
	}
	return out, nil
}
