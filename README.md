# thousandeyes

`thousandeyes` is a Go CLI for interacting with the ThousandEyes API.
It is designed to be both human-usable and automation/agent-friendly.

## Features

- Discover shorthand **`resource`** / **`verb`** commands directly from root help and nested help
- JSON output mode for machine consumption (`--json`)

## Installation

Install latest release (Linux/macOS):

```bash
curl -fsSL https://raw.githubusercontent.com/thousandeyes/thousandeyes-cli/main/scripts/install.sh | sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/thousandeyes/thousandeyes-cli/main/scripts/install.sh | TE_VERSION=v1.2.3 sh
```

By default, the script installs to `/usr/local/bin` when writable, otherwise `~/.local/bin`. Set `INSTALL_DIR` to override the destination.

The installer will overwrite the target binary. Running installation script will remove the existing `thousandeyes` binaries found on your `PATH`.

After install, the script prints shell-specific instructions for enabling tab completion.

## Build locally

Requirements:

- Go 1.24+
- A ThousandEyes bearer token

```bash
git clone https://github.com/thousandeyes/thousandeyes-cli
cd thousandeyes-cli
go build -o thousandeyes .
```

Install to PATH:

```bash
go build -o /usr/local/bin/thousandeyes .
```

## Quick Start

Set auth token:

```bash
export TE_TOKEN="<thousandeyes-bearer-token>"
```

Run:

```bash
thousandeyes --help
thousandeyes tests list
```

## Configuration

Global flags:

- `--token`: bearer token (overrides `TE_TOKEN`)

Environment variables:

- `TE_TOKEN`: ThousandEyes API bearer token used for authentication.
- `TE_BASE_URL`: ThousandEyes platform base URL without `/v7` (for example, `https://api.thousandeyes.com`).

## Shell Completion

Generate completion scripts:

```bash
thousandeyes completion bash
thousandeyes completion zsh
thousandeyes completion fish
thousandeyes completion powershell
```

Enable completion for your shell (pick one):

**bash**

```bash
source <(thousandeyes completion bash)
# or persist in ~/.bashrc:
echo 'source <(thousandeyes completion bash)' >> ~/.bashrc
```

**zsh**

```bash
# If zsh completion is not already enabled:
autoload -Uz compinit
compinit

source <(thousandeyes completion zsh)
# or persist in ~/.zshrc (after compinit):
echo 'source <(thousandeyes completion zsh)' >> ~/.zshrc
```

**fish**

```bash
mkdir -p ~/.config/fish/completions
thousandeyes completion fish > ~/.config/fish/completions/thousandeyes.fish
```

**PowerShell**

```powershell
thousandeyes completion powershell | Out-String | Invoke-Expression
# or persist in $PROFILE:
Add-Content $PROFILE 'thousandeyes completion powershell | Out-String | Invoke-Expression'
```

## Commands

### API

**Shorthand**

```text
thousandeyes <command-segment> [<command-segment> ...] [flags]
```

Use **`thousandeyes --help`** to see top-level command segments, then keep drilling down with **`thousandeyes <segment> --help`** until you reach the
verb command. If two commands share the same final verb token under one branch, that branch help lists the exact subcommand names to use.

**`thousandeyes <command path> --help`** shows path parameters, query parameters, and—when the call takes a JSON body—flags for top-level body fields
and a short example shape.

**How shorthand commands are generated**

- The CLI loads operations from `api/thousandeyes.yaml`.
- Command routing comes from `x-thousandeyes-cli-command` attributes on the effective OpenAPI spec (for example, from `api/thousandeyes.yaml` directly
  or via `api/thousandeyes.overlay.yaml`).
- Each value is a slash-separated command path. Example: `tests/http-server/list` maps to `thousandeyes tests http-server list`.
- Only operations with `x-thousandeyes-cli-command` are exposed as shorthand commands.
- Nested command groups are supported, so paths with 3+ segments create subcommand trees automatically.

**Operation parameter flags (shorthand commands)**

- Path and query parameters are exposed as first-class flags on each shorthand verb command.
- Use `thousandeyes <command path> --help` to see available parameter flags.
- Example: if a command path is `/alerts/{alertId}` and it supports query param `state`, use `--alertId <value>` and `--state <value>`.

**JSON request body (shorthand)**

Many verbs expose **one flag per top-level JSON field** (names look like **`--my-field`**). Build the request body by passing those field flags. For
array or object fields, pass the value as JSON on the flag (quoted in the shell).

Examples:

```bash
./thousandeyes --help
./thousandeyes tests list
./thousandeyes alerts get --alertId 2783
./thousandeyes alerts get --help
./thousandeyes account-groups create \
  --account-group-name "my-group"
./thousandeyes tests api create \
  --test-name "sample-api-test" \
  --type "api" \
  --requests '[{"url":"https://example.com","method":"GET"}]' \
  --predefined-variables '{"region":"eu-west"}'
```

## Project Structure

The repo is organized by command area:

- `cmd/root.go` wires the CLI together and loads shared config.
- `cmd/api/` contains shorthand OpenAPI-backed routing, request building, and API-specific tests.
- `internal/apispec/` parses and caches the OpenAPI spec used by the API shorthand commands.
- `internal/cliurls/` holds shared URL-building helpers.
- `internal/teapi/` contains the generated and raw ThousandEyes API clients.

## Development

Generate the typed OpenAPI client:

```bash
go generate ./...
```

Run checks (CI-aligned):

```bash
make ci
```

Run individual checks:

```bash
make validate
make test
make build
```

## License

Distributed under the `Apache License 2.0`. See [LICENSE](LICENSE) for more information.

## Releasing a new version

Releases are built with [GoReleaser](https://goreleaser.com/) via the **Release** GitHub Actions workflow. The workflow uses GitHub's default
`GITHUB_TOKEN` and runs only when you trigger it manually; pushing a tag alone does not start a release.

1. **Choose a version** using semantic versioning (for example `1.2.3`).

2. **Run the workflow** in GitHub: **Actions** → **Release** → **Run workflow** → select the branch or commit to ship (for example `master`) → enter
   the tag (e.g. `1.2.3`) → **Run workflow**.

The workflow creates and pushes the tag on the selected ref, then publishes a GitHub Release with binaries and checksums, as configured in
`.goreleaser.yaml`.
