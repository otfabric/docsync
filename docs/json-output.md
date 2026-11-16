# docsync JSON output

`docsync check --format json` writes the findings to standard output as one
JSON array, indented by two spaces and followed by a newline. Nothing else
goes to standard output; messages about the run itself (a usage error, a
configuration that cannot be used, a file docsync refuses to read) go to
standard error.

Each element is one finding (`internal/finding`):

| Field | Type | Meaning |
|---|---|---|
| `rule` | string | The rule, such as `DS-REF`; `docsync rules` lists them |
| `file` | string | The file the finding is about, slash-separated and relative to the checked directory |
| `line` | number | The line in `file`, from 1. Left out when the finding has no line, such as a missing document |
| `subject` | string | What the finding is about: the name, the exit code, the document. The `subject` of an `ignore` entry in `.otfabric/docsync.json` is matched against it |
| `message` | string | The finding in words |

The array is sorted by `file`, `line`, `rule` and `subject`. Without findings
it is `[]`. The strings are UTF-8; JSON escapes control characters itself, so
`file` and `message` carry them unchanged, where the other formats show them
as `\xNN` or `\uNNNN`. A byte that is not UTF-8 becomes U+FFFD.

The exit code is the one of every format ([exitcodes.md](exitcodes.md)): 0
with `[]`, 1 with findings. When docsync stops before it checks anything
(exit 2, 3, or 10 while it reads the module), standard output is empty. When a
check could not run because docsync refused a file or because its tool
could not run (exit 10 after the checks), the array holds the findings of
the checks that ran, the DS-CONF finding that says which tool included
unless `--rules` leaves it out.

From the CLI fixture (`docsync check --format json --rules DS-EXIT testdata/cli`):

```json
[
  {
    "rule": "DS-EXIT",
    "file": "docs/exitcodes.md",
    "line": 8,
    "subject": "exit 4",
    "message": "exit code 4 is documented as \"Bad weather\"; the organisation table makes it \"transport\""
  }
]
```
