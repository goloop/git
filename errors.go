package git

import (
	"errors"
	"fmt"

	"github.com/goloop/git/internal/managed"
)

// Stable errors for validation, lookup, execution limits and repository state.
var (
	ErrGitNotFound        = errors.New("git executable unavailable")
	ErrUnsupportedVersion = errors.New("unsupported git version")
	ErrNotRepository      = errors.New("not a repository root")
	ErrBareRepository     = errors.New("operation requires a worktree")
	ErrUnbornHEAD         = errors.New("unborn HEAD")
	ErrDetachedHEAD       = errors.New("detached HEAD")
	ErrObjectNotFound     = errors.New("object not found")
	ErrPushRejected       = errors.New("remote ref update rejected")
	ErrRemoteNotFound     = errors.New("remote not found")
	ErrRefNotFound        = errors.New("reference not found")
	ErrPathNotFound       = errors.New("path not found")
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrConflict           = errors.New("merge conflict")
	ErrStreamIdle         = errors.New("git stream idle timeout")
	ErrOutputLimit        = errors.New("output limit exceeded")
	ErrUnsafeRepository   = managed.ErrUnsafe
	errFormat             = errors.New("malformed git output")
)

// CommandError identifies the failed operation without leaking arguments or stderr.
type CommandError struct {
	Operation string
	Status    int
	Err       error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("git %s failed (status %d)", e.Operation, e.Status)
}

// Unwrap returns the execution error.
func (e *CommandError) Unwrap() error { return e.Err }
func status(e error, n int) bool      { var x *CommandError; return errors.As(e, &x) && x.Status == n }
