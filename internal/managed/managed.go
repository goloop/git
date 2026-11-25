// Package managed validates service-owned bare repositories.
package managed

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrUnsafe identifies unsupported metadata and paths.
var ErrUnsafe = errors.New("unsafe repository")

// IDs contain simple ASCII namespace components.
var component = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]*(?:\\.[A-Za-z0-9_-]+)*$")

// Valid reports whether an identifier can name a hosted repository.
func Valid(id string) bool {
	if len(id) > 512 || !strings.HasSuffix(id, ".git") {
		return false
	}
	for _, v := range strings.Split(id, "/") {
		if !component.MatchString(v) || !filepath.IsLocal(v) {
			return false
		}
	}
	return true
}

// Path verifies a service-owned repository. The owner must control all metadata
// and ancestor directories for the lifetime of every child process.
func Path(ctx context.Context, root, id string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !filepath.IsAbs(root) || !Valid(id) {
		return "", ErrUnsafe
	}
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	p := filepath.Join(root, filepath.FromSlash(id))
	real, e := filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	if real != p {
		return "", ErrUnsafe
	}
	for _, n := range []string{"config", "objects", "refs", "HEAD"} {
		st, e := os.Lstat(filepath.Join(p, n))
		if e != nil {
			return "", e
		}
		dir := n == "objects" || n == "refs"
		if dir && !st.IsDir() || !dir && !st.Mode().IsRegular() {
			return "", ErrUnsafe
		}
	}
	for _, n := range []string{"commondir", "gitdir", "config.worktree", "objects/info/alternates", "objects/info/http-alternates", "info/grafts"} {
		_, e := os.Lstat(filepath.Join(p, n))
		if !errors.Is(e, os.ErrNotExist) {
			return "", ErrUnsafe
		}
	}
	// Accept the small config grammar emitted by git init --bare. Reject
	// includes, command-valued settings, continuations and unknown extensions.
	f, e := os.Open(filepath.Join(p, "config"))
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil || len(b) > 8192 {
		return "", ErrUnsafe
	}
	scan := bufio.NewScanner(strings.NewReader(string(b)))
	section := ""
	bare := false
	reftable := false
	for scan.Scan() {
		s := strings.Trim(scan.Text(), " \t\r")
		if s == "" || s[0] == '#' || s[0] == ';' {
			continue
		}
		if s == "[core]" || s == "[extensions]" {
			section = s
			continue
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return "", ErrUnsafe
		}
		k = strings.Trim(k, " \t")
		v = strings.Trim(v, " \t")
		switch section + k {
		case "[core]bare":
			if v != "true" {
				return "", ErrUnsafe
			}
			bare = true
		case "[core]filemode", "[core]ignorecase", "[core]precomposeunicode", "[core]logallrefupdates", "[core]symlinks":
			if v != "true" && v != "false" {
				return "", ErrUnsafe
			}
		case "[core]repositoryformatversion":
			if v != "0" && v != "1" {
				return "", ErrUnsafe
			}
		case "[extensions]refstorage":
			reftable = v == "reftable"
			if v != "files" && v != "reftable" {
				return "", ErrUnsafe
			}
		case "[extensions]objectformat":
			if v != "sha256" && v != "sha1" {
				return "", ErrUnsafe
			}
		default:
			return "", ErrUnsafe
		}
	}
	if !bare || scan.Err() != nil {
		return "", ErrUnsafe
	}
	if reftable {
		info, err := os.Lstat(filepath.Join(p, "reftable"))
		if err != nil || !info.IsDir() {
			return "", ErrUnsafe
		}
	}
	return p, nil
}

// Env constructs the environment for a managed process. The executable and
// its installation, OS environment and filesystem are administrator-controlled.
func Env() []string {
	env := []string{"HOME=" + os.DevNull, "XDG_CONFIG_HOME=" + os.DevNull, "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_ALLOW_PROTOCOL=",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_NO_LAZY_FETCH=1",
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_CONFIG_COUNT=6",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=" + os.DevNull,
		"GIT_CONFIG_KEY_1=gc.auto",
		"GIT_CONFIG_VALUE_1=0",
		"GIT_CONFIG_KEY_2=maintenance.auto",
		"GIT_CONFIG_VALUE_2=false",
		"GIT_CONFIG_KEY_3=receive.fsckObjects",
		"GIT_CONFIG_VALUE_3=true",
		"GIT_CONFIG_KEY_4=receive.denyNonFastForwards",
		"GIT_CONFIG_VALUE_4=true",
		"GIT_CONFIG_KEY_5=receive.denyDeletes",
		"GIT_CONFIG_VALUE_5=true",
	}
	for _, k := range []string{"PATH", "SystemRoot", "TEMP", "TMP", "TMPDIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}
