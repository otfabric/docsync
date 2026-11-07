# docsync Releases

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
