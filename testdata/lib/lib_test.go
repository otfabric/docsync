package lib

import "testing"

func FuzzParse(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) {}) }
