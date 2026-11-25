// Package githttp serves smart HTTP discovery and RPC for managed bare
// repositories. Applications supply authentication, authorization and routing.
package githttp

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/goloop/git/internal/managed"
	"github.com/goloop/git/internal/process"
)

// Options controls smart HTTP. Resolve maps a URL identifier to a managed
// repository identifier under Root. Authorize is called on every request.
// Both callbacks must be safe for concurrent use.
type Options struct {
	Root, Executable string
	Resolve          func(context.Context, string) (string, error)
	Authorize        func(*http.Request, string, bool) bool
	// RemoteUser optionally supplies the authenticated account ID for receive-pack reflogs.
	RemoteUser              func(*http.Request) string
	EnablePush              bool
	MaxRequest, MaxResponse int64
	Timeout                 time.Duration
	Concurrency             int
}

// Handler runs git-http-backend for authorized requests.
type Handler struct {
	o     Options
	exe   string
	slots chan struct{}
}

// New requires an absolute service-owned root and both callbacks. Zero limits
// select 1 GiB per direction, five minutes and eight concurrent requests.
func New(o Options) (*Handler, error) {
	if !filepath.IsAbs(o.Root) || o.Resolve == nil || o.Authorize == nil || o.Concurrency < 0 || o.MaxRequest < 0 || o.MaxResponse < 0 || o.Timeout < 0 {
		return nil, errors.New("invalid smart HTTP options")
	}
	exe := o.Executable
	if exe == "" {
		exe = "git"
	} else if !filepath.IsAbs(exe) {
		return nil, errors.New("expected absolute Git executable")
	}
	exe, e := exec.LookPath(exe)
	if e != nil {
		return nil, e
	}
	if o.MaxRequest == 0 {
		o.MaxRequest = 1 << 30
	}
	if o.MaxResponse == 0 {
		o.MaxResponse = 1 << 30
	}
	if o.Concurrency == 0 {
		o.Concurrency = 8
	}
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Minute
	}
	return &Handler{o, exe, make(chan struct{}, o.Concurrency)}, nil
}
func route(r *http.Request) (id, service, suffix string, ok bool) {
	if strings.Contains(r.URL.EscapedPath(), "%") || !strings.HasPrefix(r.URL.Path, "/") {
		return
	}
	path := r.URL.Path[1:]
	switch {
	case r.Method == "GET" && strings.HasSuffix(path, "/info/refs"):
		suffix = "/info/refs"
		service = r.URL.Query().Get("service")
		if r.URL.RawQuery != "service="+service {
			return
		}
	case r.Method == "POST" && r.URL.RawQuery == "":
		pos := strings.LastIndexByte(path, '/')
		if pos < 0 {
			return
		}
		suffix = path[pos:]
		service = suffix[1:]
		if r.Header.Get("Content-Type") != "application/x-"+service+"-request" {
			return
		}
	default:
		return
	}
	id = strings.TrimSuffix(path, suffix)
	ok = managed.Valid(id) && (service == "git-upload-pack" || service == "git-receive-pack")
	return
}

// ServeHTTP authenticates discovery and RPC independently. Upstream servers
// must configure socket deadlines and TLS.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, service, suffix, ok := route(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	write := service == "git-receive-pack"
	ctx, cancel := context.WithTimeout(r.Context(), h.o.Timeout)
	defer cancel()
	r = r.WithContext(ctx)
	if write && !h.o.EnablePush || !h.o.Authorize(r, id, write) {
		http.Error(w, "forbidden", 403)
		return
	}
	user := "authenticated"
	if write && h.o.RemoteUser != nil {
		user = h.o.RemoteUser(r)
		if user == "" || strings.TrimSpace(user) != user || !utf8.ValidString(user) || strings.ContainsAny(user, "<>") || strings.IndexFunc(user, unicode.IsControl) >= 0 {
			http.Error(w, "invalid authenticated identity", http.StatusForbidden)
			return
		}
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		http.Error(w, "busy", 503)
		return
	}
	mapped, e := h.o.Resolve(ctx, id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	p, e := managed.Path(ctx, h.o.Root, mapped)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	if r.ContentLength > h.o.MaxRequest {
		http.Error(w, "request too large", 413)
		return
	}
	proto := r.Header.Get("Git-Protocol")
	if proto != "" && proto != "version=1" && proto != "version=2" {
		http.Error(w, "unsupported protocol", 400)
		return
	}
	stop := context.AfterFunc(ctx, func() { r.Body.Close() })
	defer stop()
	var body io.Reader
	length := r.ContentLength
	if r.Method == http.MethodPost {
		raw := http.MaxBytesReader(w, r.Body, h.o.MaxRequest)
		switch r.Header.Get("Content-Encoding") {
		case "", "identity":
			body = raw
		case "gzip":
			gz, err := gzip.NewReader(raw)
			if err != nil {
				http.Error(w, "invalid gzip body", 400)
				return
			}
			defer gz.Close()
			body = http.MaxBytesReader(w, gz, h.o.MaxRequest)
			length = -1
		default:
			http.Error(w, "unsupported encoding", 415)
			return
		}
	}
	cmd := exec.CommandContext(ctx, h.exe, "-c", "http.receivepack="+strconv.FormatBool(write), "http-backend")
	process.Configure(cmd, true)
	cmd.Dir = p
	cmd.Env = append(managed.Env(),
		"REQUEST_METHOD="+r.Method,
		"GIT_PROJECT_ROOT="+filepath.Dir(p),
		"PATH_INFO=/"+filepath.Base(p)+suffix,
		"QUERY_STRING="+r.URL.RawQuery,
		"GIT_HTTP_EXPORT_ALL=1",
		"CONTENT_TYPE="+r.Header.Get("Content-Type"),
		"GIT_PROTOCOL="+proto,
		"REMOTE_USER="+user,
	)
	if length >= 0 {
		cmd.Env = append(cmd.Env, "CONTENT_LENGTH="+strconv.FormatInt(length, 10))
	}
	cmd.Stdin = body
	cmd.WaitDelay = time.Second
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		http.Error(w, "backend unavailable", 502)
		return
	}
	if e = cmd.Start(); e != nil {
		http.Error(w, "backend unavailable", 502)
		return
	}
	waited := false
	defer func() {
		cancel()
		pipe.Close()
		r.Body.Close()
		if !waited {
			cmd.Wait()
		}
	}()
	rd := bufio.NewReader(pipe)
	header, code, e := headers(rd)
	if e != nil {
		http.Error(w, "invalid backend response", 502)
		return
	}
	for k, v := range header {
		w.Header()[k] = v
	}
	w.WriteHeader(code)
	n, e := io.Copy(w, io.LimitReader(rd, h.o.MaxResponse))
	if e != nil {
		panic(http.ErrAbortHandler)
	}
	if n == h.o.MaxResponse {
		if _, e = rd.ReadByte(); e != io.EOF {
			panic(http.ErrAbortHandler)
		}
	}
	waited = true
	if e = cmd.Wait(); e != nil {
		panic(http.ErrAbortHandler)
	}
}
func headers(rd *bufio.Reader) (http.Header, int, error) {
	h := http.Header{}
	code := 200
	for total := 0; total < 16384; {
		b, e := rd.ReadSlice('\n')
		if e != nil {
			return nil, 0, e
		}
		total += len(b)
		s := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		if s == "" {
			return h, code, nil
		}
		k, v, ok := strings.Cut(s, ":")
		if !ok {
			return nil, 0, errors.New("invalid CGI header")
		}
		v = strings.TrimSpace(v)
		switch http.CanonicalHeaderKey(k) {
		case "Status":
			raw, _, _ := strings.Cut(v, " ")
			code, e = strconv.Atoi(raw)
			if e != nil || code < 200 || code > 599 {
				return nil, 0, errors.New("invalid CGI status")
			}
		case "Content-Type", "Cache-Control", "Pragma", "Expires":
			h.Set(k, v)
		}
	}
	return nil, 0, errors.New("CGI headers too large")
}
