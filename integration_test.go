package git

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func check(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func env(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}
func native(t *testing.T, p string, a ...string) string {
	t.Helper()
	cmd := exec.Command("git", a...)
	cmd.Dir = p
	s, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %s: %v %s", a[0], e, s)
	}
	return line(string(s))
}
func fixture(t *testing.T, format string) (context.Context, *Client, *Repository) {
	t.Helper()
	ctx := t.Context()
	c, e := New(ClientOptions{})
	check(t, e)
	r, e := c.Init(ctx, filepath.Join(t.TempDir(), "work"), InitOptions{InitialBranch: "main", ObjectFormat: format})
	check(t, e)
	check(t, r.SetConfig(ctx, "user.name", "Test"))
	check(t, r.SetConfig(ctx, "user.email", "test@example.invalid"))
	return ctx, c, r
}
func put(t *testing.T, r *Repository, p, s string) {
	t.Helper()
	check(t, os.WriteFile(filepath.Join(r.path, p), []byte(s), 0600))
}
func read(t *testing.T, s io.ReadCloser) string {
	t.Helper()
	defer s.Close()
	b, e := io.ReadAll(s)
	check(t, e)
	return string(b)
}
func TestSnapshots(t *testing.T) {
	env(t)
	for _, hash := range []string{"sha1", "sha256"} {
		t.Run(hash, func(t *testing.T) {
			ctx, c, r := fixture(t, hash)
			h, e := r.Head(ctx)
			if !errors.Is(e, ErrUnbornHEAD) || h.Ref != "refs/heads/main" {
				t.Fatal(h, e)
			}
			name := "- файл"
			if runtime.GOOS != "windows" {
				name += "\n\xff"
			}
			content := "binary\x00data"
			put(t, r, name, content)
			st, e := r.Status(ctx)
			check(t, e)
			if len(st) != 1 || st[0].Path != name {
				t.Fatal(st)
			}
			check(t, r.Add(ctx, name))
			stats, e := r.DiffStat(ctx, DiffOptions{Staged: true})
			check(t, e)
			if len(stats) != 1 || !stats[0].Binary {
				t.Fatal(stats)
			}
			check(t, r.Commit(ctx, "first\n\nbody  \n"))
			v, e := r.CommitInfo(ctx, "HEAD")
			check(t, e)
			if v.Message != "first\n\nbody  \n" || v.Author.Name != "Test" || len(v.Parents) != 0 {
				t.Fatalf("%+v", v)
			}
			tree, e := r.Tree(ctx, "main", false)
			check(t, e)
			if len(tree) != 1 || tree[0].Path != name {
				t.Fatal(tree)
			}
			info, e := r.ObjectInfo(ctx, string(tree[0].ID))
			check(t, e)
			if info.Size != int64(len(content)) {
				t.Fatal(info)
			}
			s, e := r.Blob(ctx, string(info.ID))
			check(t, e)
			if read(t, s) != content {
				t.Fatal("blob mismatch")
			}
			b, e := r.ReadFile(ctx, "HEAD", name, 20)
			check(t, e)
			if string(b) != content {
				t.Fatal("file mismatch")
			}
			_, e = r.ReadFile(ctx, "HEAD", name, 1)
			if !errors.Is(e, ErrOutputLimit) {
				t.Fatal(e)
			}
			for _, p := range []string{"../config", "/etc/passwd", ".git/config"} {
				_, e = r.ReadFile(ctx, "HEAD", p, 30)
				if !errors.Is(e, ErrInvalidArgument) {
					t.Fatal(p, e)
				}
			}
			if runtime.GOOS != "windows" {
				check(t, os.Symlink("outside", filepath.Join(r.path, "link")))
				check(t, r.Add(ctx, "link"))
				check(t, r.Commit(ctx, "link"))
				b, e = r.ReadFile(ctx, "main", "link", 100)
				check(t, e)
				if string(b) != "outside" {
					t.Fatal(b)
				}
			}
			for _, m := range []string{"", "#42 release\nbody  \n"} {
				name := "release"
				check(t, r.CreateTag(ctx, name, "HEAD", m))
				v, e := r.TagInfo(ctx, name)
				check(t, e)
				if m != "" && (v.Tagger == nil || v.Tagger.Name != "Test" || v.Message != m || v.Target == v.ID) {
					t.Fatal(v)
				}
				if (v.Type == "tag") != (m != "") {
					t.Fatal(v)
				}
				check(t, r.DeleteTag(ctx, name))
			}
			check(t, r.CreateBranch(ctx, "topic", "main"))
			check(t, r.RenameBranch(ctx, "topic", "feature"))
			check(t, r.Switch(ctx, "feature"))
			put(t, r, "new", "line\n")
			check(t, r.Add(ctx, "new"))
			check(t, r.Commit(ctx, "feature"))
			check(t, r.Switch(ctx, "main"))
			base, e := r.MergeBase(ctx, "main", "feature")
			check(t, e)
			yes, e := r.IsAncestor(ctx, string(base), "feature")
			check(t, e)
			if !yes {
				t.Fatal(base)
			}
			_, e = r.MergeTree(ctx, "main", "feature")
			check(t, e)
			check(t, r.Merge(ctx, "feature"))
			check(t, r.DeleteBranch(ctx, "feature"))
			check(t, r.Checkout(ctx, string(v.ID)))
			h, e = r.Head(ctx)
			check(t, e)
			if h.Ref != "" || h.ID != v.ID {
				t.Fatal(h)
			}
			native(t, r.path, "pack-refs", "--all")
			refs, e := r.Refs(ctx)
			check(t, e)
			if len(refs) == 0 {
				t.Fatal(refs)
			}
			check(t, r.Switch(ctx, "main"))
			check(t, r.Fsck(ctx))
			check(t, r.GC(ctx))
			n, e := r.Size(ctx)
			check(t, e)
			if n == 0 {
				t.Fatal(n)
			}
			arc, e := r.Archive(ctx, "main", "zip")
			check(t, e)
			if !strings.HasPrefix(read(t, arc), "PK") {
				t.Fatal("zip")
			}
			bundle := filepath.Join(t.TempDir(), "backup.bundle")
			check(t, os.WriteFile(bundle, []byte(read(t, r.BundleCreate(ctx))), 0600))
			check(t, r.BundleVerify(ctx, bundle))
			refs, e = r.BundleListHeads(ctx, bundle)
			check(t, e)
			if len(refs) == 0 {
				t.Fatal(refs)
			}
			wt := filepath.Join(t.TempDir(), "linked")
			native(t, r.path, "worktree", "add", "--detach", wt)
			linked, e := c.Open(ctx, wt)
			check(t, e)
			i, e := linked.Info(ctx)
			check(t, e)
			if i.CommonDir == i.GitDir || i.ObjectFormat != hash {
				t.Fatal(i)
			}
		})
	}
}
func TestSynchronization(t *testing.T) {
	env(t)
	ctx, c, r := fixture(t, "sha1")
	put(t, r, "a", "a\n")
	check(t, r.Add(ctx, "a"))
	check(t, r.Commit(ctx, "base"))
	root := t.TempDir()
	server, e := c.Init(ctx, filepath.Join(root, "team.git"), InitOptions{Bare: true, InitialBranch: "main"})
	check(t, e)
	check(t, r.AddRemote(ctx, "origin", server.path))
	check(t, r.Push(ctx, "origin", "main"))
	rr, e := r.LsRemote(ctx, server.path)
	check(t, e)
	if len(rr) != 1 {
		t.Fatal(rr)
	}
	cloned, e := c.Clone(ctx, server.path, filepath.Join(t.TempDir(), "clone"), CloneOptions{Depth: 1})
	check(t, e)
	sh, e := cloned.IsShallow(ctx)
	check(t, e)
	if !sh {
		t.Fatal("full clone")
	}
	put(t, r, "a", "b\n")
	check(t, r.Add(ctx, "a"))
	check(t, r.Commit(ctx, "update"))
	check(t, r.Push(ctx, "origin", "main"))
	check(t, cloned.Fetch(ctx, "origin"))
	if err := cloned.Push(ctx, "origin", "main"); !errors.Is(err, ErrPushRejected) {
		t.Fatal(err)
	}
	check(t, cloned.Pull(ctx, "origin", "main"))
	check(t, cloned.RemoveRemote(ctx, "origin"))
	managed, e := c.OpenManaged(ctx, root, "team.git")
	check(t, e)
	v, e := managed.CommitInfo(ctx, "main")
	check(t, e)
	if v.Subject != "update" {
		t.Fatal(v)
	}
	if e = managed.SetConfig(ctx, "test.key", "x"); !errors.Is(e, ErrUnsafeRepository) {
		t.Fatal(e)
	}
	if _, e = managed.Status(ctx); !errors.Is(e, ErrBareRepository) {
		t.Fatal(e)
	}
	check(t, server.SetConfig(ctx, "include.path", "external"))
	_, e = c.OpenManaged(ctx, root, "team.git")
	if !errors.Is(e, ErrUnsafeRepository) {
		t.Fatal(e)
	}
}
func TestChanges(t *testing.T) {
	env(t)
	ctx, _, r := fixture(t, "sha1")
	put(t, r, "a", "hello\n")
	check(t, r.Add(ctx, "a"))
	check(t, r.Commit(ctx, "start"))
	check(t, os.Rename(filepath.Join(r.path, "a"), filepath.Join(r.path, "b")))
	check(t, r.Add(ctx, "a", "b"))
	st, e := r.Status(ctx)
	check(t, e)
	if len(st) != 1 || st[0].OriginalPath != "a" {
		t.Fatal(st)
	}
	o := DiffOptions{Staged: true, DetectRenames: true}
	ch, e := r.ChangedFiles(ctx, o)
	check(t, e)
	if len(ch) != 1 || ch[0].Path != "b" || ch[0].OriginalPath != "a" {
		t.Fatal(ch)
	}
	ds, e := r.DiffStat(ctx, o)
	check(t, e)
	if len(ds) != 1 || ds[0].OriginalPath != "a" {
		t.Fatal(ds)
	}
	patch, e := r.Diff(ctx, o)
	check(t, e)
	if !strings.Contains(read(t, patch), "rename from a") {
		t.Fatal("patch")
	}
	check(t, r.Commit(ctx, "move"))
	bl, e := r.Blame(ctx, "HEAD", "b")
	check(t, e)
	if !strings.Contains(read(t, bl), "hello") {
		t.Fatal("blame")
	}
	put(t, r, "b", "local")
	check(t, r.Restore(ctx, "b"))
	check(t, r.Remove(ctx, "b"))
	log, e := r.Log(ctx, LogOptions{Since: "HEAD~1", Paths: []string{"b"}})
	check(t, e)
	if len(log) != 1 {
		t.Fatal(log)
	}
}
func TestConcurrent(t *testing.T) {
	env(t)
	ctx, c, r := fixture(t, "sha1")
	put(t, r, "x", strings.Repeat("a", 2<<20))
	check(t, r.Add(ctx, "x"))
	check(t, r.Commit(ctx, "large"))
	v, e := r.Head(ctx)
	check(t, e)
	var wg sync.WaitGroup
	wins := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := r.CommitInfo(ctx, "main")
			if e != nil {
				t.Error(e)
			}
			wins <- r.UpdateRef(ctx, "refs/heads/race", v.ID, "") == nil
		}()
	}
	wg.Wait()
	close(wins)
	count := 0
	for win := range wins {
		if win {
			count++
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
	tree, e := r.Tree(ctx, "main", false)
	check(t, e)
	s, e := r.Blob(ctx, string(tree[0].ID))
	check(t, e)
	check(t, s.Close())
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, e = c.Version(canceled)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	_, e = New(ClientOptions{Executable: filepath.Join(t.TempDir(), "missing")})
	if !errors.Is(e, ErrGitNotFound) {
		t.Fatal(e)
	}
}
func FuzzPorcelain(f *testing.F) {
	for _, v := range []string{"? a\x00", "2 R. N... 100644 100644 100644 a b R100 next\x00old\x00", "u"} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = parseStatus(s); _, _ = parseCommits(s) })
}
