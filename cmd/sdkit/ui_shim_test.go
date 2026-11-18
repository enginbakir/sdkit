package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The session calls `sdkit` by name. Whatever this binary is called, that name must lead to it, not to another install on PATH.
func TestSdkitShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links and the .exe name differ")
	}
	dir := t.TempDir()
	odd := filepath.Join(dir, "sdkit-ui-new")
	os.WriteFile(odd, []byte("#!/bin/sh\n"), 0o755)
	shim, err := sdkitShim(odd)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(shim)
	got, err := filepath.EvalSymlinks(filepath.Join(shim, "sdkit"))
	want, _ := filepath.EvalSymlinks(odd)
	if err != nil || got != want {
		t.Errorf("sdkit in %s leads to %q, want %q (%v)", shim, got, want, err)
	}
	named := filepath.Join(dir, "sdkit")
	os.WriteFile(named, []byte("#!/bin/sh\n"), 0o755)
	if shim, _ := sdkitShim(named); shim != dir {
		t.Errorf("a binary already called sdkit needs no shim: %s", shim)
	}
}
