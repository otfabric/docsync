# docsync Releases

## v0.3.0

**Date:** 2026-11-07
**Previous release:** v0.2.0

### Added

- DS-SEC reports a `SECURITY.md` without a supported-versions table.
- DS-EXIT knows exit code 13 (safety: a state-changing operation on a live, non-virtual target without the confirmation it requires); 9 is permission only.

### Changed

- DS-REF checks `ErrX` in inline code and `go` blocks, and `pkg.ErrX` of a module package in prose; an unqualified ErrX in prose or a table (IEC 61850 ErrTerm, ErrPar) is no longer reported.
- DS-REF accepts the test functions of `_test.go` files (`Test…`, `Fuzz…`, `Benchmark…`, `Example…`, including a package-level `Example`), qualified (`devicefile.FuzzParse`) or not.
- DS-REF accepts `pkg.Name` of a dependency package the module imports under a name a local package also has (go-ocpp's `client.HandshakeError` in ocppctl, which has `cmd/client`), looked up with `go list`.
- DS-VER treats a dependency version as a requirement only when the keyword stands right before the module's name ("requires go-x v1.2.3") or the version ends in `+`; "Fixed in go-x v1.0.1" in a line that also says "requires" is not a claim. A link to the module counts as its name ("built on [otfabric/go-x](…) v1.2.3").
- DS-JSON accepts a field named as a segment of a path: `attempts[].offered`, `meta.id`, `a[].b[].c`.
- `HANDOFF-*.md` (design hand-off notes) are excluded by default.
- DS-SEC accepts a supported-versions cell that starts with the version and adds a note (`0.8.x (latest: v0.8.2)`).
- DS-SEC accepts an open-ended supported-versions row (`0.6.6 and later`, `0.6+`, `>= 0.6`) that includes the latest release tag.

### Fixed

- DS-CODE: a package name several module packages share resolves to the one declaring the selected names, and to a public package before an `internal/` one, instead of the shortest import path; when none declares them, to the dependency package of that name the module imports.
- DS-FLAG: a root flag's command is named without a trailing space.
- The library fixture no longer depends on docsync's own release tags.

---

## v0.2.0

**Date:** 2026-11-07
**Previous release:** v0.1.0

### Added

- `docsync api`: the API reference of the public packages in `docs/api/`, one file per package and an index, from the declarations and doc comments; platform-specific declarations of linux, darwin, windows and freebsd once each.
- DS-GEN: the generated reference is current. The generated files count for DS-API and are read by no other check.

### Changed

- DS-REL reports a section that copies most of the previous one (three or more bullets, more than half) instead of every repeated bullet: status lines such as "None." or "opcua-interop v0.13.0 (unchanged)" repeat.
- DS-SEC accepts a "Latest release" row in the supported-versions table.
- DS-EXIT accepts the usual synonyms of the organisation's meanings, in the table and in the constant names (`ExitConnection` is transport, "refused" is protocol, "not supported" is unsupported).
- `skip_dirs` leaves out `test/**` and `tests/**` by default.

### Fixed

- DS-CODE: `errors.New` (and any standard package) in a module that has a package of the same name resolves to the standard library when the module's package lacks the member.
- DS-CODE: a `var ( ErrX // … )` block that only lists names is checked for the names instead of failing to parse.

---

## v0.1.0

**Date:** 2026-11-07

### Added

- `docsync check`: the rules DS-API, DS-REF, DS-ERR, DS-CODE, DS-SIG, DS-VER, DS-SEC and DS-REL for Go modules, and DS-CMD, DS-FLAG, DS-EX, DS-EXIT and DS-JSON for cobra CLIs, with `--format text|github|json|markdown` and `--rules`.
- `docsync template`: the `TestDocsyncManifest` test a cobra CLI carries (manifest version 1).
- `.otfabric/docsync.json`: API documents, excluded documents, skipped package directories, CLIs, and accepted findings with their reasons.

---
