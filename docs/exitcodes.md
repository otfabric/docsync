# docsync exit codes

| Code | Meaning |
|---|---|
| 0 | Success: the documents agree with the code |
| 1 | Findings: the check ran and reports disagreements |
| 2 | Usage: unknown command, flag or format |
| 3 | Invalid input: `.otfabric/docsync.json` cannot be used |
| 10 | Internal: the module or a document cannot be read |

The codes follow the organisation table (otfabric/engineering
`standards/api-design.md`).
