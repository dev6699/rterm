# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go 1.22 module (`github.com/dev6699/rterm`) for a web-based remote terminal. Core packages are organized by responsibility:

- `rterm.go`: public registration and application setup.
- `auth/`, `command/`: authentication and command definitions.
- `server/`: HTTP and WebSocket transport.
- `tty/`: terminal process, controller, agent, and message handling.
- `ui/` and `ui/src/`: embedded web UI and xterm.js assets. The UI entrypoint is
  `ui/src/script.js`, which imports responsibility-focused modules for state,
  protocol/bridge handling, terminal lifecycle, sessions, sockets, providers,
  authentication, and transfers. Page styling lives in `ui/src/style.css`.
- `ui/test/`: Node-based UI unit tests, shared browser/WebSocket fakes, and
  entrypoint smoke coverage.
- `cmd/rterm/`: main server binary; `cmd/totp/`: TOTP utility.
- `docs/`: screenshots and demo media.

Keep package-specific code in its existing directory and update `README.md` when public behavior or usage changes.

## Build, Test, and Development Commands

Run the server locally with `make run` (or `go run cmd/rterm/main.go`). Build the distributable binary with `make build`; this uses `CGO_ENABLED=0` and writes `./rterm`. Run the TOTP utility with `make totp`. Run the UI tests with `npm run test:ui` or `npm run test:ui:coverage`. Before submitting changes, use `go test ./...`, `go vet ./...`, and `gofmt -w` on changed Go files. UI changes should include focused tests under `ui/test/` and preserve the coverage check.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms: tabs are produced by `gofmt`, exported identifiers use GoDoc comments, and names are concise `MixedCaps` rather than underscores. Keep JavaScript and CSS changes consistent with the existing `ui/src` style. Use native ES modules for browser code; keep `script.js` as the stable entrypoint and add new responsibilities as imported modules rather than rebuilding one large file. Keep page CSS in external files under `ui/src/`, referenced with the configured asset prefix. Avoid modifying vendored or generated-looking assets unless the change requires it.

## Testing Guidelines

Place Go tests beside the implementation in `*_test.go` files and name cases descriptively, such as `TestRegisterRejectsUnauthorizedEmbed`. Run the full suite with `go test ./...`; use focused package or test runs while iterating, then rerun the full suite before review.

For UI changes, organize tests by responsibility (`auth.test.js`, `terminal.test.js`, `sessions.test.js`, `sockets.test.js`, and so on). Keep shared DOM/WebSocket fakes in `ui/test/` helpers. Run both `npm run test:ui` and `npm run test:ui:coverage`. Node tests validate module behavior and entrypoint wiring, but do not replace browser/Electron visual smoke testing when that runtime is available.

## Commit & Pull Request Guidelines

Follow the existing Conventional Commit-style prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, or `test:`) with a short imperative subject. Pull requests should explain the behavior change, identify relevant packages, include test commands and results, and attach screenshots or a short recording for UI changes. Keep unrelated edits out of the change and do not overwrite existing contributor work.

## Security & Configuration Tips

Treat command registration, authentication, WebSocket handling, and terminal input as security-sensitive. Do not commit credentials, TOTP secrets, or environment-specific endpoints. Review embed and `postMessage` behavior carefully, including origin and authorization checks, before changing it.
