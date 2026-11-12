package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// changedFiles lists the .go files that differ from ref, relative to root and
// with forward slashes.
//
// The committed part is the merge-base diff (ref...HEAD), so a long-lived base
// branch does not light up every file it changed after the fork. The working
// tree is added on top: a file with uncommitted edits is also "changed since
// ref", and it lets -base HEAD describe what the user is editing right now.
// Only names are read; the base tree is never analyzed.
func changedFiles(root, ref string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	committed, err := gitDiffNames(ctx, root, ref+"...HEAD")
	if err != nil {
		return nil, err
	}
	changed := parseNameList(committed)
	// A worktree diff can fail on a bare repo or odd HEAD; the committed view
	// is still useful, so keep it rather than failing the whole feature.
	if worktree, werr := gitDiffNames(ctx, root, "HEAD"); werr == nil {
		for p := range parseNameList(worktree) {
			changed[p] = true
		}
	}
	return changed, nil
}

func gitDiffNames(ctx context.Context, root, rev string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "diff", "--name-only", rev, "--", "*.go")
	return cmd.Output()
}

func parseNameList(out []byte) map[string]bool {
	changed := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		// git prints root-relative paths; normalize separators so a Windows
		// checkout still matches the loader's Rel values.
		changed[filepath.ToSlash(filepath.Clean(p))] = true
	}
	return changed
}

// markChanges flags the routes and stages whose file is in the changed set.
// It answers "touches a file changed since ref", not "is a new route".
func markChanges(d *JData, changed map[string]bool, ref string) {
	n := 0
	for i := range d.Routes {
		r := &d.Routes[i]
		touched := changed[r.F]
		for j := range r.Chain {
			if changed[r.Chain[j].File] {
				r.Chain[j].Chg = true
				touched = true
			}
		}
		if touched {
			r.Chg = true
			n++
		}
	}
	d.Change = &JChange{Ref: ref, Files: len(changed), Routes: n}
}
