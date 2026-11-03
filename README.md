# thousandeyes

`thousandeyes` is a Go CLI for interacting with the ThousandEyes API.
It is designed to be both human-usable and automation/agent-friendly.

Note: This software communicates with Cisco ThousandEyes; use of Cisco ThousandEyes is subject to the Cisco [General Terms](https://www.cisco.com/c/dam/en_us/about/doing_business/legal/Cisco_General_Terms.pdf) and the applicable Offer Description, independent of the open source license governing this code.

## Features

- Discover shorthand **`resource`** / **`verb`** commands directly from root help and nested help
- JSON output mode for machine consumption (`--json`)
- Run HTTP and network synthetic CI checks with JSON and JUnit reports

## Installation

Install latest release (Linux/macOS):

```bash
curl -fsSL https://raw.githubusercontent.com/thousandeyes/thousandeyes-cli/main/scripts/install.sh | sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/thousandeyes/thousandeyes-cli/main/scripts/install.sh | TE_VERSION=1.2.3 sh
```

By default, the script installs to `/usr/local/bin` when writable, otherwise `~/.local/bin`. Set `INSTALL_DIR` to override the destination.

The installer will overwrite the target binary. Running installation script will remove the existing `thousandeyes` binaries found on your `PATH`.

After install, the script prints shell-specific instructions for enabling tab completion.

## Build locally

Requirements:

- Go 1.25+
- A ThousandEyes bearer token

The module selects the patched Go 1.27.3 build toolchain automatically. The container uses the same version.

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

**`thousandeyes <command path> --help`** shows path parameters, query parameters, and, when the call takes a JSON body, flags for top-level body fields
and a short example shape.

**How shorthand commands are generated**

- The CLI loads operations from `api/thousandeyes.yaml`.
- Command routing comes from `x-thousandeyes-cli-command` attributes on the effective OpenAPI spec (for example, from `api/thousandeyes.yaml` directly
  or via `api/thousandeyes.overlay.yaml`).
- Release binaries embed the spec and overlay so installed CLIs can load API commands without external `api/` files.
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

## Synthetic checks for CI and progressive delivery

Run HTTP-server and agent-to-server instant tests from a repository JSON file, such as the [example suite](examples/synthetics/thousandeyes-ci.json):

```bash
export TE_TOKEN="<thousandeyes-bearer-token>"
thousandeyes synthetics run-tests \
  --config examples/synthetics/thousandeyes-ci.json \
  --json-report results.json \
  --junit-report results.xml
```

Replace the example account group, agent IDs, and targets with values from your account before running it. Every execution creates new instant tests and consumes the applicable ThousandEyes test units. The CLI does not edit scheduled tests or reuse another execution's instant test IDs.

The same command can run from a terminal, GitHub Actions, Jenkins, GitLab, or a Kubernetes Job. The runner orchestrates tests; selected ThousandEyes Cloud or Enterprise Agents perform the measurements. Private targets require an Enterprise Agent with network access to them. The command does not provide a testing tunnel or install agents.

### Configuration and evaluation

Use the [JSON Schema](schemas/thousandeyes-ci.schema.json) for completion, field descriptions, examples, and validation in compatible editors and schema-based generators. The [example suite](examples/synthetics/thousandeyes-ci.json) already references it through `$schema`. In VS Code, completion inside `tests` offers HTTP and network test snippets. Replace their sample account group, agents, and targets before running a suite.

For another repository, copy the schema alongside your suite and set `"$schema": "./thousandeyes-ci.schema.json"`. Adjust this relative path if you store the schema elsewhere. The CLI accepts `$schema` as editor metadata and never fetches it. Schema defaults are authoring hints; the CLI applies execution defaults.

The schema covers the suite structure and the native fields inspected by the CLI. Additional native configuration and agent fields remain allowed and are validated by the ThousandEyes API. The CLI also checks constraints that the schema cannot fully express, including unique test names and agent IDs, the 200-byte UTF-8 name limit, and positive Go durations within the supported range.

The version 1 JSON format contains `accountGroupId`, an optional suite `timeout` (default `5m`), an optional `pollInterval` (default `5s`), and a nonempty `tests` array. Each test defines:

| Field | Meaning |
| --- | --- |
| `name` | Unique, single-line name without control characters. |
| `type` | `http-server` or `agent-to-server`. |
| `config` | Native instant-test request payload, including its target and `agents`. Do not include read-only fields such as `type` or `testId`. |
| `executionRule` | `blocking` (default), `non-blocking`, or `skipped`. |
| `thresholds` | Optional criteria shown below. |

| Type | Default gate | Optional thresholds |
| --- | --- | --- |
| HTTP server | A response code from 200 through 399, with no reported test error. | `expectedResponseCodes` replaces the CI's accepted codes; `maxResponseTime` limits time to first byte in milliseconds; `maxTotalTime` limits total time in milliseconds. |
| Agent to server | Measured end-to-end packet loss is zero. | `maxLoss` is a percentage; `maxLatency` limits average RTT in milliseconds; `maxJitter` limits jitter in milliseconds. |

Each test uses `config` and one flat `thresholds` object. Optional HTTP timing and network latency/jitter limits are unset when omitted or `null`; `0` is an active maximum. Packet loss still defaults to zero. Additional native configuration and agent fields are preserved in the creation request.

Every configured agent must return applicable results from one coherent round. Missing values remain pending until the suite deadline; they never become zero or success. A known primary measurement failure can fail immediately without waiting for timing values that may never arrive. Network diagnostics attached to HTTP tests do not automatically become additional gates. When testing an expected non-2xx/3xx response, also align the native payload's `desiredStatusCode` setting; a native test error remains a failed check even if its response code appears in `expectedResponseCodes`.

For agent-to-server creation, set `server` to the hostname or IP address and supply a separate integer `port` for TCP or UDP targets, as shown in the example suite. The API response may combine them as `hostname:port`.

The runner supports individual Cloud and Enterprise Agents. Cluster-agent completeness and network directions other than `to-target` are not supported. Suite definitions are validated before submitting tests. Unsupported test types, duplicate agents, incompatible thresholds, and invalid durations are rejected; the API validates the native test payload.

### Results and exit status

- Exit `0`: every blocking check passed, or was explicitly skipped. Non-blocking assertion failures remain visible in reports.
- Exit nonzero: a blocking assertion failed, execution was incomplete or timed out, an API/configuration error occurred, cancellation was requested, or a report could not be written. Infrastructure failures remain blocking even for a non-blocking test.
- `--json` prints the sanitized report to standard output. `--json-report` and `--junit-report` write files using the same results. Parent directories must already exist.
- Configuration and report paths must identify different files. Within the same directory, filenames that differ only by letter case or Unicode normalization are treated as collisions for portability.
- JUnit contains a case per agent, or a case for a test with no agent outcomes. An additional execution-error case preserves any test-level failure not represented by agent outcomes. Non-blocking assertion failures are represented as JUnit failures and have an `executionRule` property, while the CLI exit status follows the configured rule. Consumers that independently gate on JUnit must account for that distinction.

Reports contain run/test/agent identities, status, selected numeric measurements, and permitted platform links. Version 1 also includes optional `tests[].target` and `tests[].agents[].agentName` fields. Targets show the configured HTTP URL with user information, query strings, and fragments removed, or the network host and configured port. Names come from result metadata, falling back to the expanded creation response, without an extra agent lookup. Terminal output includes both fields; JUnit includes them as case properties.

Failed checks include the measured value and configured expectation, such as `HTTP response code 503 did not match expected [200]` or `responseTime 125.5 ms exceeded maximum 100 ms`. Native error details remain excluded; native HTTP errors can include the same safe response-code context.

`agents[].url` is the ThousandEyes results page, distinct from the tested target. The CLI prefers permitted API-provided links. If a result omits its link, the public `https://api.thousandeyes.com` origin can use a fallback built from validated account, test, agent, and round IDs. Custom API origins receive no generated application link.

Agent names are limited to 160 characters and targets to 2,048 characters; oversized or unrecognized targets are omitted. Reports exclude native request payloads, response headers, and arbitrary API error bodies. Control characters are removed from names, and the supplied API token is redacted from the new metadata. Target paths and agent names remain visible, so avoid embedding other secrets in them. Treat configuration files containing target credentials as secrets and supply `TE_TOKEN` through your CI's secret mechanism.

`--account-group-id`, `--timeout`, and `--poll-interval` override the corresponding suite settings. Each invocation is bounded by one suite deadline. The engine handles rate-limit responses within that deadline and does not blindly retry creation requests whose outcome is ambiguous.

### Container and Argo Rollouts

Build a local image:

```bash
docker build -t thousandeyes-cli:synthetic-ci .
docker run --rm -e TE_TOKEN \
  -v "$PWD/examples/synthetics:/config:ro" \
  thousandeyes-cli:synthetic-ci synthetics run-tests \
  --config /config/thousandeyes-ci.json --json
```

For Argo Rollouts, see [`examples/synthetics/argo-rollouts/analysis-template.yaml`](examples/synthetics/argo-rollouts/analysis-template.yaml) and [`examples/synthetics/argo-rollouts/rollout-step.yaml`](examples/synthetics/argo-rollouts/rollout-step.yaml). Build and push the image to your own registry, create a Secret with a `token` key, and create the suite ConfigMap:

```bash
kubectl create configmap thousandeyes-canary-suite \
  --from-file=thousandeyes-ci.json=examples/synthetics/thousandeyes-ci.json
```

Apply the AnalysisTemplate and add the example analysis step to the Rollout. Pass the image, account group, suite ConfigMap, and optional Secret name as template arguments. A ClusterAnalysisTemplate can use the same spec when a shared template is appropriate.

Configure the suite targets to reach the canary or preview revision explicitly. A shared weighted endpoint may route checks to the stable deployment. A Job running inside Kubernetes does not give remote agents access to cluster-internal Services; select connected Enterprise Agents for those targets. The example disables pod retries, uses one measurement, and sets the outer Job deadline longer than the CLI's five-minute timeout.

## Project Structure

The repo is organized by command area:

- `cmd/root.go` wires the CLI together and loads shared config.
- `cmd/api/` contains shorthand OpenAPI-backed routing, request building, and API-specific tests.
- `cmd/synthetics.go` exposes the synthetic CI command; `internal/synthetics/` validates suites, runs instant tests, evaluates measurements, and writes reports.
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

2. **Run the workflow** in GitHub: **Actions** → **Release** → **Run workflow** → select the branch or commit to ship (for example `main`) → enter
   the tag (e.g. `1.2.3`) → **Run workflow**.

The workflow creates and pushes the tag on the selected ref, then publishes a GitHub Release with binaries and checksums, as configured in
`.goreleaser.yaml`.
