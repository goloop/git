package git

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestConflictsAndGitlink(t *testing.T) {
	env(t)
	ctx, _, r := fixture(t, "sha1")
	put(t, r, "file", "base")
	check(t, r.Add(ctx, "file"))
	check(t, r.Commit(ctx, "base"))
	check(t, r.CreateBranch(ctx, "other", "main"))
	put(t, r, "file", "first")
	check(t, r.Add(ctx, "file"))
	check(t, r.Commit(ctx, "main"))
	check(t, r.Switch(ctx, "other"))
	put(t, r, "file", "second")
	check(t, r.Add(ctx, "file"))
	check(t, r.Commit(ctx, "other"))
	_, e := r.MergeTree(ctx, "main", "other")
	if !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if e = r.Merge(ctx, "main"); !errors.Is(e, ErrConflict) {
		t.Fatal("merge should conflict")
	}
	st, e := r.Status(ctx)
	check(t, e)
	if len(st) != 1 || st[0].Kind != "u" {
		t.Fatal(st)
	}
	put(t, r, "file", "resolved")
	check(t, r.Add(ctx, "file"))
	check(t, r.Commit(ctx, "resolved"))
	p, e := r.Parents(ctx, "HEAD")
	check(t, e)
	if len(p) != 2 {
		t.Fatal(p)
	}
	native(t, r.path, "update-index", "--add", "--cacheinfo", "160000,"+string(p[0])+",linked")
	check(t, r.Commit(ctx, "gitlink"))
	v, e := r.Tree(ctx, "HEAD", false)
	check(t, e)
	if len(v) != 2 || v[1].Mode != "160000" {
		t.Fatal(v)
	}
}
func TestFailureModes(t *testing.T) {
	env(t)
	ctx, c, r := fixture(t, "sha1")
	check(t, r.SetConfig(ctx, "example.value", "a\nb"))
	v, ok, e := r.GetConfig(ctx, "example.value")
	check(t, e)
	if !ok || v != "a\nb" {
		t.Fatal(v, ok)
	}
	conf, e := r.ListConfig(ctx)
	check(t, e)
	if len(conf) == 0 {
		t.Fatal(conf)
	}
	check(t, r.UnsetConfig(ctx, "example.value"))
	_, ok, e = r.GetConfig(ctx, "example.value")
	check(t, e)
	if ok {
		t.Fatal("unset")
	}
	put(t, r, "-x", strings.Repeat("text", 1<<18))
	check(t, r.Add(ctx, "-x"))
	check(t, r.Commit(ctx, "first"))
	for _, arg := range []string{"-h", "--all"} {
		if _, e = r.Resolve(ctx, arg); !errors.Is(e, ErrInvalidArgument) {
			t.Fatal(e)
		}
	}
	id, e := r.Resolve(ctx, "HEAD:-x")
	check(t, e)
	info, e := r.ObjectInfo(ctx, string(id))
	check(t, e)
	if info.Size != 1<<20 {
		t.Fatal(info)
	}
	if _, e = r.Blob(ctx, "HEAD"); !errors.Is(e, ErrInvalidArgument) {
		t.Fatal(e)
	}
	// Child failures after opening a stream must reach the reader.
	stream := r.stream(ctx, "cat-file", "blob", "HEAD")
	_, e = io.ReadAll(stream)
	check(t, stream.Close())
	var commandErr *CommandError
	if !errors.As(e, &commandErr) {
		t.Fatal(e)
	}
	limited, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	s, e := r.Blob(limited, string(id))
	check(t, e)
	<-limited.Done()
	_, e = io.Copy(io.Discard, s)
	check(t, s.Close())
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	tiny, e := New(ClientOptions{MaxOutput: 4})
	check(t, e)
	_, e = tiny.Version(ctx)
	if !errors.Is(e, ErrOutputLimit) {
		t.Fatal(e)
	}
	_, e = c.Open(ctx, filepath.Dir(r.path))
	if e == nil {
		t.Fatal("opened non-repository")
	}
	if runtime.GOOS != "windows" {
		fake := filepath.Join(t.TempDir(), "git")
		check(t, os.WriteFile(fake, []byte("#!/bin/sh\necho 'git version 2.20.0'\n"), 0700))
		old, e := New(ClientOptions{Executable: fake})
		check(t, e)
		_, e = old.Capabilities(ctx)
		if !errors.Is(e, ErrUnsupportedVersion) {
			t.Fatal(e)
		}
	}
}

func TestStreamConcurrency(t *testing.T) {
	env(t)
	ctx, _, repo := fixture(t, "sha1")
	put(t, repo, "file", strings.Repeat("a", 1<<20))
	check(t, repo.Add(ctx, "file"))
	check(t, repo.Commit(ctx, "stream"))
	c, err := New(ClientOptions{Concurrency: 1})
	check(t, err)
	repo, err = c.Open(ctx, repo.path)
	check(t, err)
	s, err := repo.Blob(ctx, "HEAD:file")
	check(t, err)
	defer s.Close()
	var first [1]byte
	_, err = io.ReadFull(s, first[:])
	check(t, err)
	limited, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	_, err = repo.CommitInfo(limited, "HEAD")
	check(t, err)
	queued, err := repo.Blob(limited, "HEAD:file")
	check(t, err)
	check(t, queued.Close())
	n, err := io.Copy(io.Discard, s)
	check(t, err)
	if n != (1<<20)-1 {
		t.Fatal(n)
	}
}

func TestCommandDeadline(t *testing.T) {
	c, err := New(ClientOptions{})
	check(t, err)
	ctx, cancel := c.commandContext(context.Background())
	defer cancel()
	if _, limited := ctx.Deadline(); limited {
		t.Fatal("unexpected default deadline")
	}
	c, err = New(ClientOptions{Timeout: time.Minute})
	check(t, err)
	ctx, cancel = c.commandContext(context.Background())
	defer cancel()
	if _, limited := ctx.Deadline(); !limited {
		t.Fatal("missing explicit deadline")
	}
}

func TestRepositoryDiagnostics(t *testing.T) {
	env(t)
	ctx, _, repo := fixture(t, "sha1")
	for _, err := range []error{repo.DeleteBranch(ctx, "missing"), repo.DeleteTag(ctx, "missing")} {
		var cmd *CommandError
		if !errors.Is(err, ErrRefNotFound) || !errors.As(err, &cmd) {
			t.Fatal(err)
		}
	}
	_, err := repo.RemoteInfo(ctx, "missing")
	if !errors.Is(err, ErrRemoteNotFound) {
		t.Fatal(err)
	}
}

func TestImportedDatesAndDanglingObjects(t *testing.T) {
	env(t)
	ctx, _, repo := fixture(t, "sha1")
	put(t, repo, "file", "first")
	check(t, repo.Add(ctx, "file"))
	check(t, repo.Commit(ctx, "base"))
	tree := native(t, repo.path, "rev-parse", "HEAD^{tree}")
	raw := "tree " + tree + "\nauthor Test <t@example.invalid> 1000000000 +5960\ncommitter Test <t@example.invalid> 1000000000 -0330\n\nimported\n"
	put(t, repo, "import", raw)
	id := native(t, repo.path, "hash-object", "-w", "-t", "commit", "import")
	v, err := repo.CommitInfo(ctx, id)
	check(t, err)
	_, authorZone := v.Author.When.Zone()
	_, commitZone := v.Committer.When.Zone()
	if v.Author.When.Unix() != 1000000000 || authorZone != 0 || commitZone != -12600 {
		t.Fatal(v)
	}
	// Use a separate healthy repository for integrity checks.
	_, _, healthy := fixture(t, "sha1")
	for _, s := range []string{"first", "second", "third"} {
		put(t, healthy, "blob", s)
		native(t, healthy.path, "hash-object", "-w", "blob")
	}
	c, err := New(ClientOptions{MaxOutput: 64})
	check(t, err)
	healthy.client = c
	check(t, healthy.Fsck(ctx))
}

func TestConfiguredSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based SSH fixture")
	}
	env(t)
	ctx, _, repo := fixture(t, "sha1")
	dir := t.TempDir()
	check(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\nexit 1\n"), 0700))
	check(t, os.WriteFile(filepath.Join(dir, "test-ssh"), []byte("#!/bin/sh\ntouch used-ssh\nexit 1\n"), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	check(t, repo.SetConfig(ctx, "core.sshCommand", "test-ssh"))
	_, err := repo.LsRemote(ctx, "ssh://localhost/repo")
	if err == nil {
		t.Fatal("expected transport error")
	}
	_, err = os.Stat(filepath.Join(repo.path, "used-ssh"))
	check(t, err)
}

func TestManagedReftable(t *testing.T) {
	env(t)
	ctx, c, repo := fixture(t, "sha1")
	v, err := c.Version(ctx)
	check(t, err)
	if v.Major == 2 && v.Minor < 45 {
		t.Skip("requires Git 2.45")
	}
	root := t.TempDir()
	native(t, root, "init", "--bare", "--ref-format=reftable", "-b", "main", "team.git")
	put(t, repo, "file", "reftable")
	check(t, repo.Add(ctx, "file"))
	check(t, repo.Commit(ctx, "first"))
	check(t, repo.AddRemote(ctx, "origin", filepath.Join(root, "team.git")))
	check(t, repo.Push(ctx, "origin", "main"))
	managed, err := c.OpenManaged(ctx, root, "team.git")
	check(t, err)
	refs, err := managed.Branches(ctx)
	check(t, err)
	if len(refs) != 1 || refs[0].Name != "refs/heads/main" {
		t.Fatal(refs)
	}
}
