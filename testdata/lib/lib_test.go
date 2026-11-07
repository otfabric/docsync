package lib

import (
	"testing"

	depclient "github.com/otfabric/go-dep/client"
)

var _ error = depclient.HandshakeError{}

func FuzzParse(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) {}) }
