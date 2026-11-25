package git

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
)

// ObjectID is a full native object name in the repository's hash format.
type ObjectID string

// Ref names a reference and its direct target.
type Ref struct {
	Name           string
	ID             ObjectID
	Type, Symbolic string
}

// Branch identifies a local branch.
type Branch = Ref

// Tag identifies a tag ref. TagInfo also loads the target, signature and
// message of an annotated tag. Tagger is nil when no signature is present.
type Tag struct {
	Ref
	Target  ObjectID
	Tagger  *Signature
	Message string
}

// HeadState records a symbolic branch or a detached commit.
type HeadState struct {
	Ref string
	ID  ObjectID
}

func validID(id ObjectID) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	_, e := hex.DecodeString(string(id))
	return e == nil
}
func (r *Repository) resolve(ctx context.Context, s string) (ObjectID, error) {
	if !operand(s) {
		return "", ErrInvalidArgument
	}
	out, e := r.out(ctx, "rev-parse", "--verify", "--quiet", "--end-of-options", s)
	if status(e, 1) {
		return "", ErrObjectNotFound
	}
	if e != nil {
		return "", e
	}
	id := ObjectID(line(out))
	if !validID(id) {
		return "", errFormat
	}
	return id, nil
}

// Resolve takes a snapshot of a native revision expression.
func (r *Repository) Resolve(ctx context.Context, s string) (ObjectID, error) {
	info, e := r.ObjectInfo(ctx, s)
	return info.ID, e
}
func (r *Repository) commitID(ctx context.Context, s string) (ObjectID, error) {
	if !operand(s) {
		return "", ErrInvalidArgument
	}
	return r.resolve(ctx, s+"^{commit}")
}

// SymbolicRef reads a symbolic ref. Detached HEAD yields ErrDetachedHEAD.
func (r *Repository) SymbolicRef(ctx context.Context, name string) (string, error) {
	if !operand(name) {
		return "", ErrInvalidArgument
	}
	s, e := r.out(ctx, "symbolic-ref", "-q", name)
	if status(e, 1) && name == "HEAD" {
		return "", ErrDetachedHEAD
	}
	return line(s), e
}

// Head returns the branch even when ErrUnbornHEAD accompanies it.
func (r *Repository) Head(ctx context.Context) (HeadState, error) {
	s, e := r.SymbolicRef(ctx, "HEAD")
	if e != nil && !errors.Is(e, ErrDetachedHEAD) {
		return HeadState{}, e
	}
	id, e := r.commitID(ctx, "HEAD")
	if s != "" && errors.Is(e, ErrObjectNotFound) {
		e = ErrUnbornHEAD
	}
	return HeadState{s, id}, e
}
func (r *Repository) refs(ctx context.Context, prefix string) ([]Ref, error) {
	s, e := r.out(ctx, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(objecttype)%00%(symref)", prefix)
	if e != nil {
		return nil, e
	}
	refs := []Ref{}
	if s != "" {
		for _, rec := range strings.Split(line(s), "\n") {
			v := strings.Split(rec, "\x00")
			if len(v) != 4 {
				return nil, errFormat
			}
			refs = append(refs, Ref{v[0], ObjectID(v[1]), v[2], v[3]})
		}
	}
	return refs, nil
}

// Refs reads loose and packed refs.
func (r *Repository) Refs(ctx context.Context) ([]Ref, error) { return r.refs(ctx, "refs/") }

// Branches lists local heads.
func (r *Repository) Branches(ctx context.Context) ([]Branch, error) {
	return r.refs(ctx, "refs/heads/")
}

// Tags lists tags without peeling annotated objects.
func (r *Repository) Tags(ctx context.Context) ([]Tag, error) {
	refs, e := r.refs(ctx, "refs/tags/")
	if e != nil {
		return nil, e
	}
	tags := make([]Tag, len(refs))
	for i, ref := range refs {
		tags[i] = Tag{Ref: ref}
	}
	return tags, nil
}

// TagInfo reads the named tag.
func (r *Repository) TagInfo(ctx context.Context, name string) (Tag, error) {
	ref := "refs/tags/" + name
	if e := r.checkRef(ctx, ref); e != nil {
		return Tag{}, e
	}
	refs, e := r.refs(ctx, ref)
	if e != nil {
		return Tag{}, e
	}
	for _, v := range refs {
		if v.Name == ref {
			tag := Tag{Ref: v, Target: v.ID}
			if v.Type != "tag" {
				return tag, nil
			}
			raw, e := r.out(ctx, "cat-file", "tag", string(v.ID))
			if e != nil {
				return Tag{}, e
			}
			return annotated(tag, raw)
		}
	}
	return Tag{}, ErrRefNotFound
}
func annotated(tag Tag, raw string) (Tag, error) {
	header, msg, ok := strings.Cut(raw, "\n\n")
	if !ok {
		return Tag{}, errFormat
	}
	tag.Message = msg
	var target ObjectID
	for _, field := range strings.Split(header, "\n") {
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "object":
			target = ObjectID(value)
		case "tagger":
			end := strings.LastIndex(value, "> ")
			if end < 0 {
				return Tag{}, errFormat
			}
			name, email, ok := strings.Cut(value[:end], " <")
			if !ok {
				return Tag{}, errFormat
			}
			when, err := signatureTime(value[end+2:])
			if err != nil {
				return Tag{}, err
			}
			tag.Tagger = &Signature{name, email, when}
		}
	}
	if !validID(target) {
		return Tag{}, errFormat
	}
	tag.Target = target
	return tag, nil
}
func (r *Repository) checkRef(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "refs/") {
		return ErrInvalidArgument
	}
	e := r.do(ctx, "check-ref-format", ref)
	if status(e, 1) {
		return ErrInvalidArgument
	}
	return e
}

// UpdateRef changes a direct ref atomically. Empty old requires a new ref.
func (r *Repository) UpdateRef(ctx context.Context, ref string, next, old ObjectID) error {
	if !validID(next) || old != "" && !validID(old) {
		return ErrInvalidArgument
	}
	if e := r.checkRef(ctx, ref); e != nil {
		return e
	}
	if strings.Trim(string(next), "0") == "" {
		return ErrInvalidArgument
	}
	return r.do(ctx, "update-ref", "--no-deref", ref, string(next), string(old))
}
func (r *Repository) branchName(ctx context.Context, name string) error {
	if !operand(name) || name == "HEAD" || strings.Contains(name, "@{") {
		return ErrInvalidArgument
	}
	return r.checkRef(ctx, "refs/heads/"+name)
}

// CreateBranch creates a new branch at a commit.
func (r *Repository) CreateBranch(ctx context.Context, name, rev string) error {
	if e := r.branchName(ctx, name); e != nil {
		return e
	}
	id, e := r.commitID(ctx, rev)
	if e != nil {
		return e
	}
	return r.do(ctx, "branch", "--no-track", "--", name, string(id))
}

// DeleteBranch requests Git's merged-branch deletion checks.
func (r *Repository) DeleteBranch(ctx context.Context, name string) error {
	if e := r.branchName(ctx, name); e != nil {
		return e
	}
	return r.deleteRef(ctx, "refs/heads/"+name, "branch", "-d", "--", name)
}

// RenameBranch renames a branch without replacing an existing branch.
func (r *Repository) RenameBranch(ctx context.Context, from, to string) error {
	for _, s := range []string{from, to} {
		if e := r.branchName(ctx, s); e != nil {
			return e
		}
	}
	return r.do(ctx, "branch", "-m", "--", from, to)
}

// CreateTag creates a lightweight tag or an annotated tag with message.
func (r *Repository) CreateTag(ctx context.Context, name, rev, message string) error {
	if e := r.checkRef(ctx, "refs/tags/"+name); e != nil {
		return e
	}
	id, e := r.Resolve(ctx, rev)
	if e != nil {
		return e
	}
	a := []string{"tag", "--no-sign"}
	if message != "" {
		a = append(a, "--cleanup=verbatim", "-m", message)
	}
	return r.do(ctx, append(a, "--", name, string(id))...)
}

// DeleteTag removes the named tag.
func (r *Repository) DeleteTag(ctx context.Context, name string) error {
	if e := r.checkRef(ctx, "refs/tags/"+name); e != nil {
		return e
	}
	return r.deleteRef(ctx, "refs/tags/"+name, "tag", "-d", "--", name)
}

func (r *Repository) deleteRef(ctx context.Context, ref string, args ...string) error {
	err := r.do(ctx, args...)
	if status(err, 1) && status(r.do(ctx, "show-ref", "--exists", ref), 2) {
		return errors.Join(ErrRefNotFound, err)
	}
	return err
}
