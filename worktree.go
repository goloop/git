package git

import (
	"context"
	"errors"
)

func (r *Repository) work(ctx context.Context, a ...string) error {
	if r.bare {
		return ErrBareRepository
	}
	return r.do(ctx, a...)
}
func (r *Repository) paths(ctx context.Context, a, p []string) error {
	if len(p) == 0 {
		return ErrInvalidArgument
	}
	for _, v := range p {
		if !validPath(v) {
			return ErrInvalidArgument
		}
	}
	return r.work(ctx, append(append(a, "--"), p...)...)
}

// Add stages literal paths.
func (r *Repository) Add(ctx context.Context, paths ...string) error {
	return r.paths(ctx, []string{"add"}, paths)
}

// Remove deletes tracked paths from index and worktree, subject to Git checks.
func (r *Repository) Remove(ctx context.Context, paths ...string) error {
	return r.paths(ctx, []string{"rm"}, paths)
}

// Restore overwrites the named worktree files with their staged contents.
func (r *Repository) Restore(ctx context.Context, paths ...string) error {
	return r.paths(ctx, []string{"restore", "--worktree"}, paths)
}

// Checkout detaches HEAD at rev, preserving Git's local-change checks.
func (r *Repository) Checkout(ctx context.Context, rev string) error {
	id, e := r.commitID(ctx, rev)
	if e != nil {
		return e
	}
	return r.work(ctx, "checkout", "--detach", string(id), "--")
}

// Switch checks out an existing local branch.
func (r *Repository) Switch(ctx context.Context, name string) error {
	if e := r.branchName(ctx, name); e != nil {
		return e
	}
	return r.work(ctx, "switch", "--no-guess", name)
}

// Commit records staged changes using the repository's configured identity.
func (r *Repository) Commit(ctx context.Context, message string) error {
	return r.work(ctx, "commit", "--no-gpg-sign", "--cleanup=verbatim", "-m", message)
}
func (r *Repository) pair(ctx context.Context, a, b string) ([]string, error) {
	x, e := r.commitID(ctx, a)
	if e != nil {
		return nil, e
	}
	y, e := r.commitID(ctx, b)
	if e != nil {
		return nil, e
	}
	return []string{string(x), string(y)}, nil
}

// MergeBase returns one best common ancestor.
func (r *Repository) MergeBase(ctx context.Context, a, b string) (ObjectID, error) {
	p, e := r.pair(ctx, a, b)
	if e != nil {
		return "", e
	}
	s, e := r.out(ctx, append([]string{"merge-base"}, p...)...)
	return ObjectID(line(s)), e
}

// IsAncestor reports reachability through the commit graph.
func (r *Repository) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	p, e := r.pair(ctx, a, b)
	if e != nil {
		return false, e
	}
	e = r.do(ctx, append([]string{"merge-base", "--is-ancestor"}, p...)...)
	if status(e, 1) {
		return false, nil
	}
	return e == nil, e
}

// MergeTree writes a candidate tree without updating any branch or index.
func (r *Repository) MergeTree(ctx context.Context, a, b string) (ObjectID, error) {
	p, e := r.pair(ctx, a, b)
	if e != nil {
		return "", e
	}
	s, e := r.out(ctx, append([]string{"merge-tree", "--write-tree", "--no-messages"}, p...)...)
	if status(e, 1) {
		return "", errors.Join(ErrConflict, e)
	}
	return ObjectID(line(s)), e
}

// Merge merges rev into the checked-out branch. Conflicts stay in the index.
func (r *Repository) Merge(ctx context.Context, rev string) error {
	id, e := r.commitID(ctx, rev)
	if e != nil {
		return e
	}
	err := r.work(ctx, "merge", "--no-edit", "--no-autostash", "--no-gpg-sign", string(id))
	if status(err, 1) {
		// A quiet diff reports whether the index contains an unmerged entry.
		probe := r.do(ctx, "diff", "--cached", "--quiet", "--no-ext-diff", "--diff-filter=U", "--")
		if status(probe, 1) {
			return errors.Join(ErrConflict, err)
		}
	}
	return err
}
