# docsync exit codes

| Code | Meaning |
|---|---|
| 0 | Success: the documents agree with the code |
| 1 | Findings: the check ran and reports disagreements |
| 2 | Usage: unknown command, flag, format or rule (`--rules`) |
| 3 | Invalid input: `.otfabric/docsync.json` cannot be used, or `docsync api` finds no `go.mod` |
| 10 | Internal: the module or a document cannot be read, docsync refuses a file of the checked directory (see [SECURITY.md](../SECURITY.md)), or `docsync api` refuses a path it would write |

A check whose tool cannot run (`go list` for DS-CODE, the manifest test of a
configured CLI for DS-CMD, DS-FLAG and DS-EX) reports a DS-CONF finding and
the run exits 10, naming the rule on standard error. The run goes on, with
0 or 1 from the other findings, when an `ignore` entry accepts that DS-CONF
finding, or when `--rules` names none of the rules it stops and not DS-CONF.
An unexpected internal error exits 10 too, with its stack on standard error.

The codes follow the organisation table (otfabric/engineering
`standards/api-design.md`).
