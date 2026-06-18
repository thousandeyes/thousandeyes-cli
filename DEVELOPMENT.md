# Introduction

This CLI is generated from [OpenAPI spec](api/thousandeyes.yaml) which is synced from other source. Do not edit files under `api/` by hand.

Here is the project structure of the repo. Please ensure you follow the current convention when making contributions.

| Area                                                                             | Role                                              |
|----------------------------------------------------------------------------------|---------------------------------------------------|
| `main.go`                                                                        | Program entrypoint                                |
| `cmd/root.go`                                                                    | Root command, global flags, wiring                |
| `cmd/api/`                                                                       | OpenAPI-backed shorthand commands, routing, tests |
| `internal/apispec/`                                                              | OpenAPI load/cache for discovery                  |
| `internal/teapi/`                                                                | Generated and raw API client                      |
| `internal/config/`, `internal/output/`, `internal/cliurls/`, `internal/version/` | Shared CLI behavior                               |

# Getting started

In case you'd like to submit your contribution, the following steps must be followed:

1. Fork the repository
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create a new Pull Request
