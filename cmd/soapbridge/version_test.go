package main

import "testing"

func TestModuleVersion(t *testing.T) {
	for in, want := range map[string]string{
		"0.1.1":       "v0.1.1", // what GoReleaser's {{.Version}} injects
		"v0.1.1":      "v0.1.1", // already a valid module version
		"0.2.0-beta1": "v0.2.0-beta1",
		"":            "",
	} {
		if got := moduleVersion(in); got != want {
			t.Errorf("moduleVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
