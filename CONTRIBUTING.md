# Contributing

Thanks for helping improve smsc-sim.

## Ground rules

- **Discuss first** for anything beyond a bug fix or small feature — open an
  issue so we agree on scope before you write code.
- **Keep the codec honest.** `internal/smpp` follows SMPP v3.4. If you extend it,
  cite the spec section in a comment and add a round-trip test.
- **Every behavioural change needs a test.** The integration tests in
  `internal/smsc` drive the real server over TCP with a throwaway client — add to
  them rather than mocking.
- **No new runtime dependencies** without discussion. The whole point is a small
  static binary.

## Local workflow

```bash
go test ./...
go test -race ./internal/smsc/...
gofmt -l .            # must print nothing
golangci-lint run
```

Run the binary against an example while developing:

```bash
go run ./cmd/smsc-sim -config examples/single-operator.yaml
```

## Commit / PR style

- Conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`).
- One logical change per PR. Update `CHANGELOG.md` under `## Unreleased`.
- CI (vet, build, race tests, gofmt, golangci-lint, docker build) must be green.

## Releasing (maintainers)

Tag `vX.Y.Z` on `main`; the `release` workflow runs GoReleaser to publish
binaries and the `ghcr.io` image.
