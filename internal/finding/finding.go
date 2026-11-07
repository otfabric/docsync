// SPDX-License-Identifier: MIT

// Package finding is what a check reports: one place where the documents and
// the code disagree.
package finding

import (
	"cmp"
	"slices"
)

// Finding is one disagreement. File is relative to the checked directory.
type Finding struct {
	Rule    string `json:"rule"`
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// Sort orders findings by file, line, rule and subject.
func Sort(fs []Finding) {
	slices.SortStableFunc(fs, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Rule, b.Rule),
			cmp.Compare(a.Subject, b.Subject),
		)
	})
}

// Rules describes every rule, in the order docsync rules prints them.
var Rules = []struct{ ID, Title string }{
	{"DS-API", "Every exported identifier of a public package is in the API reference"},
	{"DS-REF", "Identifiers the documents name (pkg.Name, Type.Member, ErrX) exist"},
	{"DS-ERR", "Every exported error sentinel and error type is in ERRORS.md"},
	{"DS-CODE", "Go code blocks parse and type-check against the module"},
	{"DS-SIG", "Signatures written in the documents match the code"},
	{"DS-VER", "Go and OT Fabric dependency versions in the documents match go.mod"},
	{"DS-SEC", "SECURITY.md names security@otfabric.io and supports the latest minor"},
	{"DS-REL", "RELEASE.md: no bullet repeated from the previous release, no stale untagged section"},
	{"DS-CMD", "Every command of a CLI is documented"},
	{"DS-FLAG", "Every flag of a CLI is documented"},
	{"DS-EX", "CLI command lines in the documents run: known commands, known flags, required flags set"},
	{"DS-EXIT", "docs/exitcodes.md matches the exit-code constants and the organisation table"},
	{"DS-JSON", "docs/json-output.md names every field of the JSON output types"},
	{"DS-CONF", "The docsync configuration and the CLI manifest are usable"},
}
