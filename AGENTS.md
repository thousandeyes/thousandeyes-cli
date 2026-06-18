# AI Agent Guidelines

These guidelines are specific to the `thousandeyes-cli` repository.

## Git Workflow

### CRITICAL: Git Safety Rules

- **NEVER** commit or push without explicit user approval
- **ALWAYS** ask before `git add`, `git commit`, `git push`, or `git commit --amend`
- Only proceed with git operations after clear confirmation

**Quick Reference:**

- Branch:
    - `feat/`: For new features (e.g., `feat/add-new-command`)
    - `fix/`: For bug fixes (e.g., `fix/fix-error-message`)
    - `hotfix/`: For urgent fixes (e.g., `hotfix/security-patch`)
    - `chore/`: For non-code tasks like dependency, docs updates (e.g., `chore/update-dependencies`)
- Commit: `<Brief description>` with bullet points in body
- Always check for uncommitted changes before creating branches

## Development Approach

**Start Small -> Verify -> Scale:**

1. Make changes to one file/component first
2. Validate behavior locally
3. Expand changes once the first pattern is correct

### CLI Design References

- When making design decisions (command naming, flags, output formatting, and UX flows), use well-known CLIs as references.
- Prefer conventions familiar from tools like `git`, `kubectl`, `docker`, `gh`, and `aws` so behavior is intuitive for experienced CLI users.

### Cleanup Policy

- When removing or deprecating a command group/feature, do not leave dead code behind.
- Remove unreferenced command packages, helper packages, docs entries, CI exclusions, and dependency entries in the same change whenever safe.
- Verify with repository-wide reference searches and `go test ./...` before finalizing.

## Repository Structure

`thousandeyes-cli` is a single Go CLI repository (not a service/deployment multi-repo layout).

- **Entrypoints**
    - `main.go` starts the binary.
    - `cmd/root.go` wires global flags/config and top-level commands.
- **Commands**
    - `cmd/api/` contains the OpenAPI-backed shorthand command wiring attached at the root command (routing, request mapping, formatting, and tests).
- **Internal packages**
    - `internal/teapi/` contains generated/raw API client code.
    - `internal/apispec/` parses/caches the OpenAPI spec for shorthand command discovery.
    - `internal/config/`, `internal/output/`, `internal/cliurls/`, `internal/version/` support shared CLI behavior.
- **API schema and generation**
    - `api/thousandeyes.yaml` is the source OpenAPI spec.
    - This repo uses `oapi-codegen` for API client generation; configuration options are documented at
      `https://pkg.go.dev/github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen`.
    - `oapi-codegen.yaml` configures generated client output.
    - `go generate ./...` regenerates API client artifacts.
- **Build/CI files**
    - `Makefile` provides local development and CI targets (`deps`, `validate`, `test`, `build`, `ci`).
    - `.github/workflows/` defines GitHub Actions CI, security, build, and release automation.

When modifying behavior, prefer keeping command wiring in `cmd/` and reusable logic in `internal/`.

## Code Style

- Follow Go conventions and existing repository patterns
- Prefer readable, explicit logic over clever abstractions
- Add comments only when they clarify non-obvious intent (*why*, not *what*)
- If a tricky/hacky/specific workaround is required (for example, escaping behavior or third-party parsing quirks), add a short inline code comment
  explaining why the workaround exists.
- When visual differentiation requires color, use `#F15E22` as the primary color and `#578596` as the secondary color. Regular text should have no
  color set.
- When creating new Go templates, prioritize consistency with existing comment style as the primary decision factor.

## Testing and Validation

- Run targeted tests first, then full suite when changes are broad
- Core local checks:
    - `go test ./...`
    - `go build ./...`
- CI-aligned checks via `Makefile`:
    - `make validate`
    - `make test`
    - `make build`
    - `make ci`
- If API schema/client-related files are touched, run:
    - `go generate ./...`
