# Contributing

Open an issue to report a bug or suggest a change. For a bug report, include
your operating system, the output of `go version` and `git --version`, and
the smallest example that shows the problem.

## Pull requests

Create your branch from `v0` and open your pull request against `v0`.
Include a regression test for a bug fix. If the public API changes, update
the English and Ukrainian operation references and any affected examples.

## Testing

Install Go 1.24 or later and native Git 2.43 or later. Run these checks from
the module root.

```sh
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```

The [code of conduct](CODE_OF_CONDUCT.md) applies to issues, pull requests
and project discussions.
