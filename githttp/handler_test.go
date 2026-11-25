package githttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func call(t *testing.T, dir string, wantOK bool, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	if (e == nil) != wantOK {
		t.Fatalf("git %s: %v %s", args[0], e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestSmartHTTP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	root := t.TempDir()
	call(t, root, true, "init", "--bare", "-b", "main", "app.git")
	call(t, filepath.Join(root, "app.git"), true, "config", "core.logallrefupdates", "true")
	var permit atomic.Bool
	h, e := New(Options{Root: root, EnablePush: true, RemoteUser: func(*http.Request) string { return "team-member" },
		Resolve: func(ctx context.Context, id string) (string, error) { return id, nil }, Authorize: func(r *http.Request, id string, write bool) bool { return id == "app.git" && (!write || permit.Load()) }})
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	local := t.TempDir()
	address := srv.URL + "/app.git"
	call(t, local, true, "clone", address, "one")
	one := filepath.Join(local, "one")
	call(t, one, true, "config", "user.name", "Tester")
	call(t, one, true, "config", "user.email", "test@example.invalid")
	data := make([]byte, 2<<20)
	if _, e := rand.Read(data); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(one, "payload.bin"), data, 0600); e != nil {
		t.Fatal(e)
	}
	call(t, one, true, "add", "payload.bin")
	call(t, one, true, "commit", "-m", "first")
	call(t, one, false, "push", "origin", "HEAD:main")
	permit.Store(true)
	call(t, one, true, "push", "origin", "HEAD:main")
	if actor := call(t, filepath.Join(root, "app.git"), true, "reflog", "show", "-1", "--format=%gn", "refs/heads/main"); actor != "team-member" {
		t.Fatal(actor)
	}
	call(t, local, true, "clone", address, "two")
	two := filepath.Join(local, "two")
	got, e := os.ReadFile(filepath.Join(two, "payload.bin"))
	if e != nil || !bytes.Equal(got, data) {
		t.Fatal("clone payload mismatch", e)
	}
	base := call(t, two, true, "rev-parse", "HEAD")
	call(t, one, true, "commit", "--allow-empty", "-m", "second")
	call(t, one, true, "push", "origin", "HEAD:main")
	call(t, two, true, "fetch", "origin")
	if base == call(t, two, true, "rev-parse", "origin/main") {
		t.Fatal("fetch missed update")
	}
	permit.Store(false)
	for _, path := range []string{"/other.git/info/refs?service=git-upload-pack", "/app.git/info/refs?service=git-receive-pack", "/app.git/config", "/../app.git/info/refs?service=git-upload-pack", "/app.git/git-receive-pack"} {
		method := "GET"
		if strings.HasSuffix(path, "-pack") && !strings.Contains(path, "?") {
			method = "POST"
		}
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatal(path, w.Code)
		}
	}
	// A read-only handler denies receive-pack even to an authorized writer.
	opts := h.o
	opts.EnablePush = false
	permit.Store(true)
	ro, e := New(opts)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	ro.ServeHTTP(w, httptest.NewRequest("GET", "/app.git/info/refs?service=git-receive-pack", nil))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if e = os.Symlink(filepath.Join(root, "app.git"), filepath.Join(root, "alias.git")); e == nil {
		opts.Resolve = func(context.Context, string) (string, error) { return "alias.git", nil }
		other, e := New(opts)
		if e != nil {
			t.Fatal(e)
		}
		w = httptest.NewRecorder()
		other.ServeHTTP(w, httptest.NewRequest("GET", "/app.git/info/refs?service=git-upload-pack", nil))
		if w.Code != 404 {
			t.Fatal(w.Code)
		}
	}
}
