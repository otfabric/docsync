package main

import (
	"os"
	"testing"
)

// TestDocsyncManifest writes a fixed command tree.
func TestDocsyncManifest(t *testing.T) {
	out := os.Getenv("DOCSYNC_MANIFEST")
	if out == "" {
		t.Skip("DOCSYNC_MANIFEST is not set")
	}
	data := `{"version":1,"name":"democtl","commands":[
{"path":"","runnable":false,"subcommands":["read","write","secret","help"],"flags":[{"name":"format","local":true},{"name":"debug","bool":true,"local":true},{"name":"verbose","shorthand":"v","bool":true,"local":true},{"name":"help","shorthand":"h","bool":true,"local":true}]},
{"path":"read","aliases":["get"],"runnable":true,"flags":[{"name":"url","shorthand":"u","required":true,"local":true},{"name":"count","local":true},{"name":"format"},{"name":"verbose","shorthand":"v","bool":true}]},
{"path":"write","runnable":true,"flags":[{"name":"value","local":true},{"name":"format"}]},
{"path":"secret","hidden":true,"runnable":true},
{"path":"help","runnable":true}
]}`
	if err := os.WriteFile(out, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
