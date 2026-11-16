# Security policy

## Reporting a vulnerability

Report privately to security@otfabric.io, or through GitHub Security Advisories
on this repository (<https://github.com/otfabric/docsync/security/advisories/new>).
Do not open a public issue. Expect an acknowledgement within 5 working days.

## Supported versions

| Version | Supported |
|---|---|
| Latest release | ✓ |
| Older releases | ✗ |

## Threat model

docsync reads a repository it is pointed at: its Go source, its Markdown and
its `go.mod`. It builds and runs code of that repository:

- `go list -export` compiles the module's packages (and downloads its
  dependencies) to type-check the Go blocks of the documents;
- for a configured CLI, `go test -run TestDocsyncManifest` runs the
  repository's tests of that package.

Treat docsync like `go test`: run it only on code you would build. In CI it
runs in the job of the repository it checks, with that job's permissions.

## Secure defaults

`docsync check` writes nothing in the checked repository. The scratch module
of the type check and the manifest file live in the system's temporary
directory and are removed afterwards. docsync needs no token and sends
nothing anywhere; `go` contacts the module proxy for dependencies the module
cache does not hold.

### What docsync reads

docsync reads the files of the checked directory through one reader,
confined to that directory with an `os.Root` (`internal/repofs`):

- It reads only regular files inside the directory. A symbolic link is
  followed only when its target resolves inside the directory; a link with
  an absolute target never does.
- It refuses a link that leaves the directory, a named pipe, a device, a
  socket, and a directory where a file is expected. A configured path that
  leaves the directory (`exit_codes`, `json_types`) is refused the same way.
- One file may have at most 64 MiB (`repofs.MaxFileSize`). One run reads at
  most 1 GiB in all (`repofs.MaxTotalSize`) and lists at most 1,048,576
  directory entries (`repofs.MaxEntries`). A file over a limit is refused
  from its size, before it is read.

A refused file stops the run with exit code 10 and a message on standard
error that names it, quoted and escaped. It is never skipped.

### What docsync api writes

`docsync api` changes only `docs/api/`, and only regular files there:

- `docs` and `docs/api` must be directories of the tree, not symbolic links;
  a missing one is created.
- Every file it replaces or removes must be a regular file in `docs/api/`.
  It writes a new file and renames it over the old one, and removes a file
  by its own name, so it follows no link.
- Only a file in `docs/api/` that starts with the generated marker counts
  as generated; anywhere else it is an ordinary document, which
  `docsync api` leaves alone.

It checks every path before the first change. When one fails, it changes
nothing and exits 10, naming the path.

### What docsync prints

File names and messages are printed with control characters, the Unicode
bidirectional controls and bytes that are not UTF-8 shown as `\xNN` or
`\uNNNN`, so each finding is one line. The `github` format escapes them as
GitHub's workflow commands require (`%`, CR and LF everywhere; `:` and `,` in
the `file=` property too). The `json` format leaves the escaping to JSON.

## Known security limitations

- A malicious repository can run code through its manifest test, or through
  cgo code that `go list -export` compiles (`go generate` is not run). Do not
  point docsync at untrusted code outside a sandbox.
- The confinement covers docsync's own reads. The tools it starts read the
  repository as they always do: `go list -export` and `go test` (the module,
  its dependencies and local `replace` targets) and `git` (tags and history).
  docsync also reads, where `go list` finds them, the source of dependency
  packages (the module cache or a `replace` directory) to resolve the names
  the documents use, and their export data in the build cache.
- The type check runs `go list` with `-mod=mod` in its scratch module, so
  `go` may also contact the module proxy for a module that a Go block of the
  documents imports although the checked module does not require it.
- The `text` format is meant for a terminal. A line starts with the file
  name, so in a CI log use `--format github`, which escapes it.
