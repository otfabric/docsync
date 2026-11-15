# docsync exit codes

| Code | Meaning |
|---|---|
| 0 | Success: the documents agree with the code |
| 1 | Findings: the check ran and reports disagreements |
| 2 | Usage: unknown command, flag or format |
| 3 | Invalid input: `.otfabric/docsync.json` cannot be used, or `docsync api` finds no `go.mod` |
| 10 | Internal: the module or a document cannot be read, docsync refuses a file of the checked directory (see [SECURITY.md](../SECURITY.md)), or `docsync api` refuses a path it would write |

A check whose tool cannot run (`go list` for DS-CODE, the manifest test of a
configured CLI) reports a DS-CONF finding, so the run exits 1.

The codes follow the organisation table (otfabric/engineering
`standards/api-design.md`).
