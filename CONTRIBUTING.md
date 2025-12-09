# Contributing to codexctl

Thank you for your interest in contributing to `codexctl`!
Contributions of all kinds are welcome: bug reports, documentation fixes, small improvements and new features.

## Prerequisites

- Go `1.25+` installed and in your `PATH`.
- A working Git setup.
- (Optional) `golangci-lint`, `staticcheck`, and `govulncheck` for additional checks.

## Development workflow

1. Fork the repository and clone your fork:

   ```bash
   git clone https://github.com/<your-username>/codexctl.git
   cd codexctl
   ```

2. Create a branch for your change:

   ```bash
   git switch -c feature/my-change
   ```

3. Implement your changes, keeping code style consistent with existing code.

4. Run formatting and basic checks before opening a PR:

   ```bash
   go fmt ./...
   go vet ./...
   go test ./...
   ```

5. For more thorough checks (recommended, but not required), you can also run:

   ```bash
   go test -race ./...
   golangci-lint run
   staticcheck ./...
   govulncheck ./...
   ```

6. Build the CLI locally if needed:

   ```bash
   go build ./cmd/codexctl
   ```

7. Commit your changes with a clear, descriptive message and push the branch to your fork.

8. Open a pull request against the `main` branch of `codex-k8s/codexctl` and describe:
   - what changed and why;
   - how you tested the change;
   - any impact on documentation or examples.

## Coding and documentation guidelines

- Keep functions small and focused; avoid unnecessary complexity.
- Prefer clear, explicit naming over abbreviations.
- Write doc comments for exported types and functions in English.
- Update `README.md` / `README_RU.md` when behavior or CLI interface changes.

## Reporting issues

When filing an issue, please include:

- the version of `codexctl` you are using;
- your Go version and OS;
- a minimal reproduction (commands, `services.yaml` snippets, logs) when possible.

Bug reports and suggestions are very helpful even if you do not plan to send a PR. Thank you for helping improve `codexctl`!

