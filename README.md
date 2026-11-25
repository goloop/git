# git

[Українська](README.UK.md) · [Operation reference](DOC.md) · [Changelog](CHANGELOG.md)

Use native Git from Go to inspect a checkout, manage a bare repository, or
serve authorized smart HTTP requests. The core and the optional `githttp`
package use only the Go standard library.

## Installation

Go 1.24+ and native Git 2.43+ must be installed on the host. The module
path is `github.com/goloop/git`. For the local v0.2.0 development copy, add
the module to your Go workspace with `go work use /path/to/git`.
After the release tag is published, install with:

```sh
go get github.com/goloop/git@v0.2.0
```

## Read a checkout

```go
func subject(ctx context.Context, path string) (string, error) {
    client, err := git.New(git.ClientOptions{})
    if err != nil { return "", err }
    repo, err := client.Open(ctx, path)
    if err != nil { return "", err }
    head, err := repo.Head(ctx)
    if errors.Is(err, git.ErrUnbornHEAD) { return "(empty repository)", nil }
    if err != nil { return "", err }
    commit, err := repo.CommitInfo(ctx, string(head.ID))
    return commit.Subject, err
}
```

## Provision a bare repository

Run provisioning with a trusted path and parent directory. The service owns
the entire repository, including its config, refs, hooks and object store.

```go
repo, err := client.Init(ctx, "/srv/git/tools.git", git.InitOptions{
    Bare: true, InitialBranch: "main",
})
if err != nil { return err }
_, err = repo.Info(ctx)
return err
```

## Stream a snapshot

```go
func copyFile(ctx context.Context, repo *git.Repository, dst io.Writer) error {
    blob, err := repo.Blob(ctx, "main:README.md")
    if err != nil { return err }
    defer blob.Close()
    _, err = io.Copy(dst, blob)
    return err
}
```

Use `ReadFile` for a small, literal repository-relative path with a byte
limit. Follow errors from both the initial operation and the stream reader.
Use `errors.Is` for repository state and validation errors. Use
`errors.As(err, &cmdErr)` with `var cmdErr *git.CommandError` to inspect
an operation and exit status. Diagnostics omit arguments and child stderr.

## Private hosting

Mount the smart HTTP handler under a prefix. Supply a resolver for internal
repository IDs and an authorization function for the authenticated request.
Authenticate in middleware before the handler. Send HTTP 401 with a
WWW-Authenticate challenge when the client needs to supply credentials.
The handler returns 403 when authorization fails. Granting read access
grants access to repository history.

```go
h, err := githttp.New(githttp.Options{
    Root: "/srv/git", Resolve: resolveRepository, Authorize: authorizeRequest,
    EnablePush: true,
})
if err != nil { return err }
mux.Handle("/git/", http.StripPrefix("/git", h))
```

[The runnable browser example](examples/browser/main.go) exposes a JSON tree
and a read-only Git endpoint on loopback. From this module run
`REPO_ROOT=/srv/git REPO_ID=tools.git REPO_PASSWORD=... go run ./examples/browser`.
Authenticate as `reader` at `/tree` or clone `/git/tools.git`. The application
owns team membership, TLS, sessions and its React or other presentation layer.

## Execution boundaries

`Open`, `Clone`, config, filesystem paths and transport arguments are for
trusted local automation. Git may execute hooks, filters, credential helpers
and SSH commands from trusted configuration. Use `OpenManaged` for browsing
service-created bare repositories. It accepts a limited configuration and
runs offline. Neither API isolates a malicious filesystem. Keep native Git
patched and isolate tenants with OS permissions, CPU, memory, disk and
network limits. Import objects through Git transport into service-owned
repositories. Never serve arbitrary uploaded repository directories.

Linux, macOS and Windows are targeted. Unix cancellation kills the Git
process group. On Windows it terminates the direct process. Deploy a job
object or equivalent worker isolation when descendant termination is needed.
See the [capability matrix and limits](DOC.md) for bare support, stream
ownership, minimum versions and backup scope.
