# Repository Guidelines

## Project Structure & Module Organization

Epsh is a Go 1.21 command-line tool for building and deploying FTC robot projects. `main.go` starts the application; `cmd/` contains Cobra command definitions and flags, while `internal/` contains all implementation packages. Keep command wiring thin and put reusable behavior in the relevant `internal/<package>/` package. Tests live beside the code as `*_test.go`; package fixtures belong in a local `testdata/` directory (for example, `internal/robotcfg/testdata/real.xml`). Root scripts such as `build.sh`, `release.sh`, and `install.sh` support packaging and releases.

## Build, Test, and Development Commands

- `make deps` downloads Go module dependencies.
- `make build` builds the local `./epsh` binary with version linker flags.
- `make test` runs the full verbose test suite (`go test -v ./...`).
- `go test ./internal/robotcfg` runs one package while iterating.
- `go fmt ./...` formats all Go source; run `go vet ./...` before opening a PR.
- `make dev` runs the tool directly with `go run main.go`; `make release` cross-compiles release artifacts into `dist/`.

Do not commit generated binaries or `dist/` output. Hardware and deployment work can touch a real robot; test with fixtures and mockable boundaries where possible before manual ADB testing.

## Coding Style & Naming Conventions

Use idiomatic Go and let `gofmt` define indentation and layout. Use package names that are short, lowercase, and purpose-based (`hotreload`, `robotcfg`). Export only APIs needed across packages; exported identifiers use `PascalCase`, unexported identifiers use `camelCase`, and errors should be returned rather than causing panics. Keep OS-specific behavior in suffixed files with build tags, such as `wifi_darwin.go` and `wifi_linux.go`.

## Testing Guidelines

Write table-driven unit tests when inputs have meaningful variants. Name tests `Test<Behavior>` and keep them in the same package as the code under test. Cover successful behavior, invalid input, and error paths, particularly for configuration parsing, ADB/Gradle calls, and platform-specific logic. There is no stated coverage threshold; new behavior should include focused tests and the full suite must pass.

## Commit & Pull Request Guidelines

Recent history uses short, imperative subjects such as `Keep what a kept class needs to compile`; follow that style and keep each commit focused. Pull requests should explain the user-facing change, identify testing performed, link related issues when applicable, and include terminal output or screenshots for TUI/visual changes. Call out any required robot, ADB, or FTC-project setup so reviewers can reproduce the change.
