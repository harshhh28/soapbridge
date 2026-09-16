# Contributing to soapbridge

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

`go test ./...` includes a couple of tests (`gateway.TestLive_*`,
`mcpgen.TestMCPOverStdio`) that call real public demo SOAP services. They
skip themselves cleanly if the network or the service is unreachable, so a
failure there is worth a second look but isn't automatically a regression
in this repo.

Lint with [golangci-lint](https://golangci-lint.run) (config in
`.golangci.yml`, same as CI):

```sh
golangci-lint run ./...
```

## Adding a WSDL fixture

If you hit a WSDL construct the parser doesn't handle (or mishandles),
please add a minimal fixture reproducing it under `testdata/wsdl/` — either
trimmed from the real WSDL (strip anything not needed to reproduce the
issue) or hand-written like `testdata/wsdl/enumsample.wsdl`, plus a test in
`wsdl/parse_test.go` asserting the expected resolved shape.

If the construct is out of v1 scope (see README's "Explicit non-goals"),
the parser should emit a `Result.Warnings` entry naming it rather than
silently mis-mapping it — that's the behavior to test for instead.

## Architecture rule

`wsdl` → `model` is the only place WSDL/XSD gets parsed. `schema`,
`gateway`, `mcpgen`, and `openapigen` all read `model.Definition`; none of
them should reach back into WSDL/XSD concepts directly. If you're adding a
feature that needs new information from the WSDL, it goes into `model`
first, then gets consumed downstream — don't let REST, MCP, and OpenAPI
generation grow separate, divergent representations of the same operation.

## Commit messages / PRs

Explain *why*, not just *what* — especially for parser changes, where the
WSDL construct that motivated the change is exactly the context a reviewer
needs.

## Publishing a release

Releases are built by [GoReleaser](https://goreleaser.com) (config:
`.goreleaser.yml`), triggered by `.github/workflows/release.yml` on any
`v*` tag push. It cross-compiles `soapbridge` for macOS/Linux ×
amd64/arm64, embeds the tag into the binary as `main.cliVersion` (this is
what lets `generate` depend on a real published soapbridge version instead
of needing a local checkout — see `cmd/soapbridge/main.go`), creates a
GitHub Release with the archives attached, and pushes an updated Homebrew
formula to the tap repo.

**One-time setup**, before the first release:
1. Push this repo to `github.com/harshhh28/soapbridge` (public).
2. Create an empty repo at `github.com/harshhh28/homebrew-soapbridge` —
   GoReleaser writes `Formula/soapbridge.rb` into it. The `homebrew-`
   prefix is a Homebrew convention: it's what makes `brew tap
   harshhh28/soapbridge` resolve to that repo.
3. Create a GitHub Personal Access Token (fine-grained, scoped to just the
   `homebrew-soapbridge` repo, `contents: write`) and add it as a repo
   secret on `soapbridge` named `HOMEBREW_TAP_GITHUB_TOKEN` — the
   workflow's default `GITHUB_TOKEN` only has access to the repo it runs
   in, not the separate tap repo.

**Every release** after that:
```sh
git tag v0.1.0
git push origin v0.1.0
```
The workflow does the rest. Once it finishes, both of these work:
```sh
go install github.com/harshhh28/soapbridge/cmd/soapbridge@latest
brew install harshhh28/soapbridge/soapbridge
```
