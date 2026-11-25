package git

import (
	"context"
	"github.com/goloop/git/internal/managed"
)

// OpenManaged opens a service-owned bare repository for offline inspection.
// Root is absolute and id is a relative slash-separated name ending in .git.
// The service must own the repository metadata and prevent filesystem races.
func (c *Client) OpenManaged(ctx context.Context, root, id string) (*Repository, error) {
	p, e := managed.Path(ctx, root, id)
	if e != nil {
		return nil, e
	}
	cc := *c
	cc.managed = true
	if _, e = cc.Capabilities(ctx); e != nil {
		return nil, e
	}
	return cc.open(ctx, p)
}
func managedCommand(op string) bool {
	switch op {
	case "version", "rev-parse", "symbolic-ref", "for-each-ref", "check-ref-format", "log", "ls-tree", "cat-file", "diff", "blame", "merge-base", "count-objects":
		return true
	}
	return false
}
