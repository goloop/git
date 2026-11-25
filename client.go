// Package git provides context-aware access to repositories through native Git.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goloop/git/internal/managed"
	"github.com/goloop/git/internal/process"
)

// ClientOptions controls trusted native Git execution. Zero limits select defaults.
type ClientOptions struct {
	Executable  string
	MaxOutput   int64
	Concurrency int
	// StreamConcurrency limits streaming processes separately. Zero uses Concurrency.
	StreamConcurrency int
	// Timeout bounds each process, including queue time. Zero uses the caller context.
	Timeout     time.Duration
	Interactive bool
}

// Client is an immutable configuration shared by repositories.
type Client struct {
	exe         string
	opts        ClientOptions
	slots       chan struct{}
	streamSlots chan struct{}
	managed     bool
}

// New locates Git. An explicit executable must be an absolute path.
func New(o ClientOptions) (*Client, error) {
	if o.Timeout < 0 || o.MaxOutput < 0 || o.Concurrency < 0 || o.StreamConcurrency < 0 {
		return nil, ErrInvalidArgument
	}
	name := o.Executable
	if name == "" {
		name = "git"
	} else if !filepath.IsAbs(name) {
		return nil, ErrInvalidArgument
	}
	exe, e := exec.LookPath(name)
	if e != nil {
		return nil, ErrGitNotFound
	}
	exe, e = filepath.Abs(exe)
	if e != nil {
		return nil, e
	}
	if o.MaxOutput == 0 {
		o.MaxOutput = 16 << 20
	}
	if o.Concurrency == 0 {
		o.Concurrency = 16
	}
	if o.StreamConcurrency == 0 {
		o.StreamConcurrency = o.Concurrency
	}
	return &Client{exe: exe, opts: o, slots: make(chan struct{}, o.Concurrency), streamSlots: make(chan struct{}, o.StreamConcurrency)}, nil
}

// Version describes the native Git release.
type Version struct{ Major, Minor, Patch int }

// Version reads the installed Git version.
func (c *Client) Version(ctx context.Context) (Version, error) {
	s, e := c.output(ctx, "", "version")
	var v Version
	if e != nil {
		return v, e
	}
	_, e = fmt.Sscanf(s, "git version %d.%d.%d", &v.Major, &v.Minor, &v.Patch)
	return v, e
}

// Capabilities describes supported native features.
type Capabilities struct {
	Version           Version
	SHA256, MergeTree bool
}

// Capabilities checks the Git 2.43 baseline used by the package.
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	v, e := c.Version(ctx)
	if e != nil {
		return Capabilities{}, e
	}
	if v.Major < 2 || v.Major == 2 && v.Minor < 43 {
		return Capabilities{Version: v}, ErrUnsupportedVersion
	}
	return Capabilities{v, true, true}, nil
}

type bounded struct {
	bytes.Buffer
	max      int64
	overflow bool
}

func (b *bounded) Write(p []byte) (int, error) {
	if int64(len(p)) > b.max-int64(b.Len()) {
		b.overflow = true
		return 0, ErrOutputLimit
	}
	return b.Buffer.Write(p)
}
func (c *Client) run(ctx context.Context, dir string, w io.Writer, args ...string) error {
	return c.execute(ctx, dir, nil, w, args...)
}
func (c *Client) execute(ctx context.Context, dir string, input io.Reader, w io.Writer, args ...string) error {
	ctx, stop := c.commandContext(ctx)
	defer stop()
	return c.executeIn(ctx, c.slots, dir, input, w, args...)
}
func (c *Client) commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.opts.Timeout > 0 {
		return context.WithTimeout(ctx, c.opts.Timeout)
	}
	return context.WithCancel(ctx)
}
func (c *Client) executeIn(ctx context.Context, slots chan struct{}, dir string, input io.Reader, w io.Writer, args ...string) error {
	if c.managed && !managedCommand(args[0]) {
		return ErrUnsafeRepository
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	base := []string{"--no-pager", "--literal-pathspecs", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "submodule.recurse=false"}
	cmd := exec.CommandContext(ctx, c.exe, append(base, args...)...)
	process.Configure(cmd, !c.opts.Interactive || c.managed)
	cmd.Stdin = input
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			cmd.Env = append(cmd.Env, pair)
		}
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_NOSYSTEM=1")
	if !c.opts.Interactive {
		cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS_REQUIRE=never")
	}
	if c.managed {
		cmd.Env = managed.Env()
	}
	cmd.Stderr = io.Discard
	var e error
	if dst, ok := w.(*io.PipeWriter); ok {
		e = runStream(ctx, cmd, dst)
	} else {
		cmd.Stdout = w
		e = cmd.Run()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if e != nil {
		if errors.Is(e, ErrOutputLimit) {
			return ErrOutputLimit
		}
		var exit *exec.ExitError
		status := -1
		if errors.As(e, &exit) {
			status = exit.ExitCode()
		}
		return &CommandError{args[0], status, e}
	}
	return nil
}

// Drain stdout before Wait so WaitDelay cannot truncate a slow consumer's
// pending bytes after Git has exited. Cancellation also releases the reader.
func runStream(ctx context.Context, cmd *exec.Cmd, dst *io.PipeWriter) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	if err = cmd.Start(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { stdout.Close() })
	defer stop()
	_, copyErr := io.Copy(dst, stdout)
	if copyErr != nil {
		stdout.Close()
	}
	if err = cmd.Wait(); err != nil {
		return err
	}
	return copyErr
}

func (c *Client) output(ctx context.Context, dir string, args ...string) (string, error) {
	return c.query(ctx, dir, nil, args...)
}
func (c *Client) query(ctx context.Context, dir string, input io.Reader, args ...string) (string, error) {
	b := bounded{max: c.opts.MaxOutput}
	e := c.execute(ctx, dir, input, &writeOnly{&b}, args...)
	if b.overflow {
		return "", ErrOutputLimit
	}
	if e != nil {
		return "", e
	}
	return b.String(), nil
}

// Hide embedded Buffer.ReadFrom so all writes observe the limit.
type writeOnly struct{ io.Writer }
type stream struct {
	*io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *stream) Close() error {
	s.once.Do(func() { s.cancel(); s.PipeReader.Close(); <-s.done })
	return nil
}
func (r *Repository) stream(ctx context.Context, args ...string) io.ReadCloser {
	ctx, cancel := r.client.commandContext(ctx)
	rd, wr := io.Pipe()
	s := &stream{PipeReader: rd, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer cancel()
		stop := context.AfterFunc(ctx, func() { wr.CloseWithError(ctx.Err()) })
		defer stop()
		e := r.client.executeIn(ctx, r.client.streamSlots, r.path, nil, wr, args...)
		wr.CloseWithError(e)
	}()
	return s
}
func number(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
func line(s string) string           { return strings.TrimSuffix(s, "\n") }
func records(s string) ([]string, error) {
	if s == "" {
		return []string{}, nil
	}
	if !strings.HasSuffix(s, "\x00") {
		return nil, errFormat
	}
	return strings.Split(s[:len(s)-1], "\x00"), nil
}
func operand(s string) bool { return s != "" && s[0] != '-' && !strings.ContainsRune(s, 0) }
