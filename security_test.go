package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedBoundary(t *testing.T) {
	env(t)
	ctx := t.Context()
	c, err := New(ClientOptions{})
	check(t, err)
	root := t.TempDir()
	p := filepath.Join(root, "store.git")
	repo, err := c.Init(ctx, p, InitOptions{Bare: true})
	check(t, err)
	for _, name := range []string{"../store.git", "/store.git", "a//store.git", "a/./store.git", "a\\store.git", "store.git/config"} {
		_, err := c.OpenManaged(ctx, root, name)
		if !errors.Is(err, ErrUnsafeRepository) {
			t.Errorf("%q: %v", name, err)
		}
	}
	alt := filepath.Join(p, "objects", "info", "alternates")
	check(t, os.WriteFile(alt, []byte(root), 0600))
	_, err = c.OpenManaged(ctx, root, "store.git")
	if !errors.Is(err, ErrUnsafeRepository) {
		t.Fatal(err)
	}
	check(t, os.Remove(alt))
	t.Setenv("GIT_DIR", t.TempDir())
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	r, err := c.OpenManaged(ctx, root, "store.git")
	check(t, err)
	if !r.IsBare() {
		t.Fatal("ambient config changed layout")
	}
	for _, key := range []string{"core.hooksPath", "filter.sample.clean", "credential.helper", "core.sshCommand", "extensions.partialClone", "core.fsmonitor"} {
		check(t, repo.SetConfig(ctx, key, "dummy"))
		_, err = c.OpenManaged(ctx, root, "store.git")
		if !errors.Is(err, ErrUnsafeRepository) {
			t.Fatal(key, err)
		}
		check(t, repo.UnsetConfig(ctx, key))
	}
}

func TestRefLockAndDiagnostics(t *testing.T) {
	env(t)
	ctx, _, r := fixture(t, "sha1")
	put(t, r, "file", "file")
	check(t, r.Add(ctx, "file"))
	check(t, r.Commit(ctx, "first"))
	h, e := r.Head(ctx)
	check(t, e)
	lock := filepath.Join(r.path, ".git", "refs", "heads", "locked.lock")
	check(t, os.WriteFile(lock, nil, 0600))
	err := r.UpdateRef(ctx, "refs/heads/locked", h.ID, "")
	var cmd *CommandError
	if !errors.As(err, &cmd) {
		t.Fatal(err)
	}
	_, err = r.Resolve(ctx, "refs/heads/locked")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatal(err)
	}
	// An unsupported protocol fails locally and can carry URL credentials.
	check(t, r.SetConfig(ctx, "protocol.missing.allow", "never"))
	err = r.AddRemote(ctx, "bad", "missing://user:private@example.invalid/repo")
	check(t, err)
	err = r.Fetch(ctx, "bad")
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
}

func TestParserBoundaries(t *testing.T) {
	for _, v := range []string{"? name", "? \x00", "2 R. N... 100644 100644 100644 a b R100 new\x00", "bad\x00"} {
		if _, err := parseStatus(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	if _, err := parseCommits("truncated\x00"); err == nil {
		t.Fatal("accepted incomplete commit")
	}
}
