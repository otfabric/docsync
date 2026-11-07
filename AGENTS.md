# AGENTS.md — docsync

## What this is

docsync checks that the documents of a Go module say what its code does (API
reference, errors, Go blocks, signatures, versions, security contact, release
notes, and for cobra CLIs commands, flags, examples, exit codes and JSON
fields). It is the doc-sync check every OT Fabric library and CLI runs in CI.
Pure Go, standard library only. Profile: **cli**.

## Organisation standards

This repository follows **otfabric/engineering policy 1.3**, profile **cli**
(`.otfabric/policy.yaml`, mode `enforce`). Where this file and the
organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Rules, flags, configuration, exit codes | README.md |
| What each rule reports, exactly | `testdata/lib`, `testdata/cli` and the golden files in `internal/cli/testdata` |
| The manifest a CLI carries | `internal/cli/manifest_test.go.tmpl` (`docsync template`) |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`) |

## Hard rules

1. No third-party dependency: docsync runs with `go run` in every repository.
2. A new finding kind gets a fixture case and a golden-file update in the same change.
3. A false positive is fixed in docsync (or made configurable), never hidden in the fixture.
4. The manifest format is versioned: a change that a CLI's copied template cannot read is version 2.
5. Never lower `COVER_MIN`, add lint suppressions, or skip or delete tests to make CI pass.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| a rule's output on purpose | `make update-golden`, then review the diff |

## Definition of Done

`make check` green; the fixtures cover the change; README.md updated for new rules, keys or flags; RELEASE.md section for user-visible changes.

## Git and releases

- Releases run from the `Release` workflow on `main` (bump `patch` by default, `minor` or `major`).
- Consumers pin a version (`go run github.com/otfabric/docsync@vX.Y.Z`); a rule that reports more is a minor release.
