// Command browser serves one managed repository to a loopback client over
// smart HTTP and exposes its HEAD tree as JSON for a web frontend.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/goloop/git"
	"github.com/goloop/git/githttp"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	root, id, password := os.Getenv("REPO_ROOT"), os.Getenv("REPO_ID"), os.Getenv("REPO_PASSWORD")
	if password == "" {
		log.Fatal("set REPO_PASSWORD")
	}
	client, e := git.New(git.ClientOptions{})
	if e != nil {
		log.Fatal(e)
	}
	authorized := func(r *http.Request) bool {
		user, pass, ok := r.BasicAuth()
		return ok && user == "reader" && subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
	}
	h, e := githttp.New(githttp.Options{Root: root, Resolve: func(_ context.Context, name string) (string, error) {
		if name != id {
			return "", errors.New("unknown repository")
		}
		return id, nil
	}, Authorize: func(r *http.Request, name string, write bool) bool { return name == id && !write && authorized(r) }})
	if e != nil {
		log.Fatal(e)
	}
	mux := http.NewServeMux()
	mux.Handle("/git/", http.StripPrefix("/git", h))
	mux.HandleFunc("GET /tree", func(w http.ResponseWriter, r *http.Request) {
		repo, e := client.OpenManaged(r.Context(), root, id)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		tree, e := repo.Tree(r.Context(), "HEAD", false)
		if e != nil {
			http.Error(w, "tree unavailable", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tree)
	})
	server := &http.Server{Addr: "127.0.0.1:8080", ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.Header().Set("WWW-Authenticate", `Basic realm="repository"`)
			http.Error(w, "authentication required", 401)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	log.Fatal(server.ListenAndServe())
}
