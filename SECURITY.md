# Security policy

## Reporting a vulnerability

Report privately to security@otfabric.io, or through GitHub Security Advisories
on this repository (<https://github.com/otfabric/docsync/security/advisories/new>).
Do not open a public issue. Expect an acknowledgement within 5 working days.

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x | ✓ |

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

docsync writes nothing in the checked repository. The scratch module of the
type check and the manifest file live in the system's temporary directory and
are removed afterwards. It needs no token and sends nothing anywhere; `go`
contacts the module proxy only for dependencies the module cache does not
hold.

## Known security limitations

- A malicious repository can run code through its manifest test, or through
  cgo code that `go list -export` compiles (`go generate` is not run). Do not
  point docsync at untrusted code outside a sandbox.
