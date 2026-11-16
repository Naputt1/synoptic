package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fake gh reports the base and head; when both commits exist locally the
// resolver does not fetch and uses their merge-base.
func TestResolvePR(t *testing.T) {
	dir := changeFixture(t)
	out, err := gitOutput(context.Background(), dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	if _, err := gitOutput(context.Background(), dir, "update-ref", "refs/remotes/origin/main", sha); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
cat <<'JSON'
{"number":42,"baseRefName":"main","baseRefOid":"%s","headRefOid":"%s","url":"https://example.test/pull/42"}
JSON
`, sha, sha)
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	spec, note, err := resolvePR(dir, "42")
	if err != nil {
		t.Fatalf("resolvePR: %v", err)
	}
	if note != "" {
		t.Fatalf("note = %q, want no fetch", note)
	}
	if spec.Mode != comparePR || spec.PR != 42 || spec.PRURL != "https://example.test/pull/42" {
		t.Fatalf("spec = %+v, want PR 42", spec)
	}
	if spec.Base != sha || spec.Head != sha {
		t.Fatalf("base/head = %s / %s, want %s", spec.Base, spec.Head, sha)
	}
	if spec.From != "origin/main" || spec.To != sha {
		t.Fatalf("labels = %s..%s, want origin/main..%s", spec.From, spec.To, sha)
	}
}

func TestResolvePRMissingGH(t *testing.T) {
	dir := changeFixture(t)
	t.Setenv("PATH", t.TempDir())
	if _, _, err := resolvePR(dir, "42"); err == nil {
		t.Fatal("resolvePR succeeded without gh")
	}
}

func TestCompareFlagsValidation(t *testing.T) {
	if _, _, _, err := compareFromFlags(t.TempDir(), "main", "", "42"); err == nil {
		t.Fatal("-pr accepted -base")
	}
	if _, _, _, err := compareFromFlags(t.TempDir(), "", "main", ""); err == nil {
		t.Fatal("-to accepted without -base")
	}
	if _, _, _, err := compareFromFlags(t.TempDir(), "", "", ""); err != nil {
		t.Fatalf("no comparison: %v", err)
	}
}
