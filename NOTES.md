# MEW — build, test, lint notes

MEW is a Wails v2 desktop app: a Go backend (`main` package at the repo root, plus
`internal/mediafire`) and a Vue 3 + Vite frontend in `frontend/`. Target platform
is Windows (the Discord IPC code is Windows-only via `golang.org/x/sys/windows`),
but the Go code also builds on other platforms (`debug_other.go`).

## Toolchain (as found on this machine)

- Go 1.26.5 windows/amd64 (`go.mod` requires `go 1.26.5`)
- Node v24.15.0 / npm 11.12.1
- Wails CLI is **not** installed here, so `wails build` cannot run locally.
  `go build ./...` / `go vet ./...` / `go test ./...` are the checks used instead;
  CI (`.github/workflows/dev.yml`, `release.yml`) runs `wails build` on
  windows-latest with Go 1.26 and Node 20.

## Commands

Build (Go):

```bash
go build ./...
```

Static analysis / linter:

```bash
go vet ./...
```

Tests:

```bash
go test ./...
go test ./... -count=1 -v      # verbose, one run per test
go test -run TestName ./...    # a single test
```

Formatting check:

```bash
gofmt -l .
```

> NOTE: this clone has CRLF line endings in the working tree (see
> `.gitattributes`, which only marks `*.uidx` as binary), so `gofmt -l` lists
> nearly every Go file even though the committed content is gofmt-clean. To get a
> real answer use `gofmt -l` after normalising, e.g.
> `git ls-files '*.go' | xargs -I{} sh -c 'tr -d "\r" < {} > /tmp/x.go && gofmt -l /tmp/x.go'`.
> This is a repository line-ending artifact, not a code issue.

Frontend:

```bash
cd frontend
npm install
npm run build        # vite build (also what wails build runs)
```

Dependency audit:

```bash
cd frontend && npm audit
go list -m all | tail -n +2   # no go.mod advisories tool installed; see ISSUES.md
```

## Baseline (recorded before any change)

| Check | Result |
| --- | --- |
| `go build ./...` | pass, exit 0, no output |
| `go vet ./...` | pass, exit 0, no output |
| `go test ./...` | pass — 34 top-level tests, 7 skipped (`TestSubwayRed*` skip when the real SUBWAYRED pack is not installed in `%APPDATA%`), 0 failures |
| `gofmt -l .` | lists 22 files — false positive from CRLF working tree (see note above) |
| `cd frontend && npm run build` | pass |
| `cd frontend && npm audit` | **9 vulnerabilities (8 high, 1 moderate)** — vue/@vue/server-renderer, nanoid, picomatch (via vite/fdir/tinyglobby), postcss, source-map-js |

No CI workflow ran the Go tests (both workflows only built); `dev.yml` now runs
`go vet` + `go test` before the build.

## Final (after the audit changes)

| Check | Result |
| --- | --- |
| `go build ./...` | pass, exit 0, no output |
| `go vet ./...` | pass, exit 0, no output |
| `go test ./...` | pass — 47 top-level tests (40 pass, 7 skip), 0 failures; `TestSubwayRed*` still skip without the real pack, and the two vanilla-mirror tests skip only when the mirror is unreachable |
| gofmt (CR-stripped copies of every `*.go`) | clean, no files listed |
| `cd frontend && npm run build` | pass, same single chunk-size warning as baseline |
| `cd frontend && npm audit` | 4 high, 0 critical/moderate — down from 9 (8 high, 1 moderate); the four left are picomatch, which has no fixed release (ISSUES.md SEC-3b) |

Not run here: `wails build` (the Wails CLI is not installed on this machine) and
any browser/GUI interaction with the app, which needs a Windows desktop session.
