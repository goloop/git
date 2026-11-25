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
	if e = r.Merge(ctx, "main"); e == nil {
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
	b, e := r.Blob(ctx, "HEAD")
	check(t, e)
	_, e = io.ReadAll(b)
	check(t, b.Close())
	var cmd *CommandError
	if !errors.As(e, &cmd) {
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
