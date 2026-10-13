# AGENTS.md

Guidance for coding agents working on tsctl.

- Go + Cobra. Commands in `cmd/`, the API client in `internal/tsapi/`.
- Never print, log or return a credential: not in output, errors, `--json` or
  `--debug`. `--debug` logs only method, path and status.
- Every write goes through `tsapi.Client.Do`, which enforces `--read-only` before
  sending. Keep it that way.
- New endpoints: add a typed helper in `internal/tsapi`, a command in `cmd/`, a fake
  in `client_test.go`, and an example in the README.
- Before committing: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`.
- Release: bump nothing in code (GoReleaser sets the version from the tag), push a
  `vX.Y.Z` tag, then update `Formula/tsctl.rb` in `dorkitude/homebrew-tap` with the
  tag tarball's URL and sha256.
