# docsync

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)

docsync checks that the documents of a Go module say what its code does. It
reads the module's source and its Markdown, and reports every place where they
disagree: an exported identifier missing from the API reference, a function
the documents name that does not exist, a Go example that no longer compiles,
a signature that changed, a stale version, a CLI flag or command line that no
longer works, an exit-code table or JSON field list that drifted from the code.

It is the doc-sync check of the OT Fabric libraries and CLIs: CI runs it on
every change, including changes to Markdown only. It needs nothing but the Go
toolchain: no dependencies, no network access beyond what `go list` needs to
load the checked module's own dependencies.

## Run it

```sh
go run github.com/otfabric/docsync@v0.2.0 check .
```

or install it:

```sh
go install github.com/otfabric/docsync@v0.2.0
docsync check --format github .
```

| Command | What it does |
|---|---|
| `docsync check [dir]` | Check the module in `dir` (default `.`) |
| `docsync api [dir]` | Write the API reference of the module's public packages to `docs/api/` (see below) |
| `docsync template [package]` | Print the `TestDocsyncManifest` test a cobra CLI adds (package clause `main` by default) |
| `docsync rules` | List the rules |
| `docsync version` | Print the version |

| Flag of `check` | Meaning |
|---|---|
| `--format text\|github\|json\|markdown` | `text` (default): `file:line: RULE: message`; `github`: workflow annotations; `json`: the findings as an array; `markdown`: a table for `$GITHUB_STEP_SUMMARY` |
| `--rules DS-API,DS-REF` | Report only these rules |

Exit codes: 0 when the documents agree with the code, 1 with findings; see
[docs/exitcodes.md](docs/exitcodes.md).

## Rules

| Rule | Checks |
|---|---|
| DS-API | Every exported identifier of a public package appears in the API reference (`API.md`, `docs/API.md`, `docs/api/**`). Methods of exported types count; typed constants are documented with their type; generated files, `internal/`, `cmd/`, examples and test helpers are left out; so are the methods of standard interfaces (`String`, `Error`, `MarshalJSON`, …). |
| DS-GEN | The generated API reference (`docs/api/`, written by `docsync api`) is what `docsync api` writes now: a changed doc comment, signature or package makes it stale. Only checked in a module that has one. |
| DS-REF | What the documents name exists: `pkg.Name` and `T.Method` in inline code, and every `ErrX` anywhere. Placeholders (`ErrXxx`, `MyStruct`) and notation that only looks like Go (`LN.DO`, `A.B.C` paths) are skipped. |
| DS-ERR | Every exported error sentinel (`ErrX`, `errors.New`) and error type (a type with `Error() string`) of a public package is in `ERRORS.md`. |
| DS-CODE | Every `go` code block parses and type-checks against the module: a whole file, declarations, statements (wrapped in a function), or both. Locals the example does not declare (`ctx`, `client`, `err`) and elided values (`...`) are accepted; what is wrong with the module's API is not. A `var ( … )` block that only lists names is checked for the names. A block that is not Go should say so (`text`). |
| DS-SIG | A block of declarations without bodies (an API listing: `func (c *Client) Read(...) ...`, `type Config struct { ... }`, or method specifications) matches the code: the function, method, field or type exists, with the same parameter and result types. A parameter written by name only (`Close(ctx) error`) matches any type. |
| DS-VER | "Requires Go 1.26", "Go 1.26+" and "Go 1.26 or later" match the `go` (or `toolchain`) directive; "built on go-x v1.2.3", "requires go-x v1.2.3+" match the version `go.mod` requires of that OT Fabric module. |
| DS-SEC | OT Fabric addresses are `@otfabric.io`; `SECURITY.md` names `security@otfabric.io` and its supported-versions table supports the minor of the latest release tag (an `X.Y.x` row, or a "Latest release" row). |
| DS-REL | `RELEASE.md`: no section that copies most of the previous release (three or more bullets, more than half, word for word; status lines such as "None." repeat freely), and the newest section is tagged within a day of being committed. |
| DS-CMD | Every command of a CLI is mentioned in its documents (README.md and `docs/` by default). |
| DS-FLAG | Every flag of a CLI is mentioned as `--name`. |
| DS-EX | Every command line of the CLI in a `sh`, `bash`, `shell`, `zsh` or `console` block runs: known commands, known flags, required flags set. A console session whose output shows exit code 2 documents a usage error and is skipped. |
| DS-EXIT | `docs/exitcodes.md` lists exactly the codes of the `Exit…` constants, and codes 0–10 mean what the organisation table says they mean (0 ok, 1 findings, 2 usage, 3 invalid input, 4 transport, 5 timeout, 6 protocol, 7 partial, 8 unsupported, 9 permission, 10 internal). Usual synonyms count: a connection or network failure is transport, refused or rejected is protocol, not supported is unsupported. |
| DS-JSON | `docs/json-output.md` names every JSON field (`` `name` `` or `"name"`) of the JSON output types. |
| DS-CONF | The configuration and the CLI manifest can be used. |

The checks that need tags and history (DS-SEC's table, DS-REL's untagged
section) run only in a git work tree with its tags: check out with
`fetch-depth: 0` in CI.

Documents of history and plans are not checked: `RELEASE.md` (apart from
DS-REL), `CHANGELOG.md`, `PLAN.md`, `ROADMAP.md`, `PROGRESS.md`, ADRs,
`DECISIONS.md`, `RESEARCH.md` and `*_DESIGN.md`. Neither are nested modules
(directories with their own `go.mod`): run docsync in each.

## Configuration

Everything is optional. `.otfabric/docsync.json`:

```json
{
  "api_docs": ["API.md", "docs/client-guide.md"],
  "errors_doc": "ERRORS.md",
  "exclude_docs": ["RELEASE.md", "docs/adr/**"],
  "skip_dirs": ["internal/**", "cmd/**", "examples/**"],
  "clis": [
    {
      "name": "democtl",
      "manifest": "./cmd",
      "exit_codes": "internal/errs",
      "json_types": "internal/output"
    }
  ],
  "ignore": [
    {"rule": "DS-REF", "subject": "Server.ServerStatus", "reason": "an OPC UA browse path, not Go"}
  ]
}
```

| Key | Meaning | Default |
|---|---|---|
| `api` | `false` for a module without a public Go API: DS-API, DS-REF, DS-ERR and DS-SIG are off | `true` |
| `api_docs` | Globs of the API reference | `API.md`, `docs/API.md`, `docs/api/**` |
| `errors_doc` | The error reference | `ERRORS.md` |
| `exclude_docs` | Globs of documents no check reads | the history and plan documents above |
| `skip_dirs` | Globs of package directories that need no reference entries | `internal/**`, `**/internal/**`, `cmd/**`, `examples/**`, `example/**`, `e2e/**`, `interop/**`, `conformance/**`, `tools/**`, `scripts/**`, `test/**`, `tests/**`, `testdata/**` |
| `clis[].name` | The command, as users type it | |
| `clis[].manifest` | The package holding `TestDocsyncManifest` | |
| `clis[].exit_codes` | The directory of the `Exit…` constants | DS-EXIT then checks the table only |
| `clis[].exit_doc` | The exit-code table | `docs/exitcodes.md` |
| `clis[].json_types` | The directory of the JSON output types | DS-JSON off |
| `clis[].json_doc` | The JSON output reference | `docs/json-output.md` |
| `clis[].docs` | Globs of the CLI's documents | `README.md`, `docs/**` |
| `ignore` | Accepted findings: `rule` (or `*`), `subject` (a glob of the finding's subject) and a `reason`, which is required | |

Globs are slash-separated: `*` matches within a directory, `**` across
directories, and `dir/**` matches `dir` and everything below it.

## The generated API reference

```sh
docsync api .
```

writes one Markdown file per public package to `docs/api/` (the package's import path, constants, variables, functions, types and methods, each with its declaration and doc comment, like `go doc`) and an index, `docs/api/README.md`. The doc comments are the documentation; hand-written guides (`API.md`) explain how the pieces fit. Every file starts with `<!-- Code generated by docsync api. DO NOT EDIT. -->`. Run it again after changing the API: it rewrites what changed and removes the files of packages that are gone, and DS-GEN fails until it has run.

The reference covers the packages DS-API covers (not `internal/`, `cmd/`, examples, tests, generated files) on linux, darwin, windows and freebsd: a name declared for several platforms appears once, the first in that order. No other check reads the generated files: they come from the code.

## CLIs: the manifest

docsync learns a cobra CLI's commands and flags from a test the CLI carries:

```sh
docsync template cmd > cmd/docsync_manifest_test.go
```

Then set the root command on the line marked `docsync:root` (for example
`root := rootCmd` or `root := NewRootCmd()`). The test is skipped in an
ordinary `go test` run; docsync runs it with `DOCSYNC_MANIFEST` set, and it
writes the command tree there. It uses cobra and pflag only, which the CLI
already requires.

## In CI

The shared workflows of [otfabric/.github](https://github.com/otfabric/.github)
run docsync: as a job of `go-ci.yml`, and as `docs-check.yml` for pushes and
pull requests that change Markdown only. In another repository:

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0
- uses: actions/setup-go@v7
  with:
    go-version-file: go.mod
- run: go run github.com/otfabric/docsync@v0.2.0 check --format github .
```

## Contributing

`make check` runs everything CI runs. The fixture modules in `testdata/` hold
one example of every finding; `make update-golden` rewrites the expected
output after an intended change. See [SECURITY.md](SECURITY.md) for
reporting a vulnerability and [RELEASE.md](RELEASE.md) for the history.

## License

MIT, see [LICENSE](LICENSE).
