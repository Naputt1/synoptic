package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// compareMode names where the two sides of a comparison come from.
type compareMode string

const (
	compareWorktree compareMode = "worktree"
	compareRange    compareMode = "range"
	comparePR       compareMode = "pr"
)

// compareSpec is the resolved comparison behind -base, -to and -pr.
//
// From and To are labels for the report; Base and Head are the refs that are
// actually materialized. For a PR, Base is the merge-base commit and Head is
// the PR head, so routes added on the base branch after the fork do not show
// up as removals.
type compareSpec struct {
	Mode  compareMode
	From  string
	To    string
	Base  string
	Head  string
	PR    int
	PRURL string
}

func worktreeSpec(ref string) compareSpec {
	return compareSpec{Mode: compareWorktree, From: ref, Base: ref}
}

func rangeSpec(from, to string) compareSpec {
	return compareSpec{Mode: compareRange, From: from, To: to, Base: from, Head: to}
}

const (
	// maxChangeFiles caps the baked file list; the count still reports every
	// changed .go file.
	maxChangeFiles = 200
	// maxChangeCommits caps the baked commit list. CommitsMore carries the rest.
	maxChangeCommits = 100
	// maxPatchBytes caps one embedded diff. The file list still names the file.
	maxPatchBytes = 48 * 1024
	// patchTruncated marks a patch that hit maxPatchBytes.
	patchTruncated = "\n… patch truncated …"
)

// changedFilesFor lists the .go files that differ between the two sides and
// their git status letters.
//
// -base alone keeps the merge-base diff (ref...HEAD) plus uncommitted edits, so
// a base branch that moved after the fork does not light up every file it
// touched. A commit range is literal (A..B). A PR uses the merge-base commit
// already resolved by resolvePR.
func changedFilesFor(root string, spec compareSpec) (map[string]bool, []JFileChange, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var rev string
	switch spec.Mode {
	case compareRange:
		rev = spec.Base + ".." + spec.To
	case comparePR:
		rev = spec.Base + ".." + spec.Head
	default:
		rev = spec.Base + "...HEAD"
	}

	out, err := gitDiffStatus(ctx, root, rev)
	if err != nil {
		return nil, nil, 0, err
	}
	status := parseNameStatus(out)

	// Uncommitted edits count for -base alone; that is what makes -base HEAD
	// describe the working tree. The working-tree status wins when a file is
	// both committed and edited.
	if spec.Mode == compareWorktree {
		if worktree, werr := gitDiffStatus(ctx, root, "HEAD"); werr == nil {
			for p, s := range parseNameStatus(worktree) {
				status[p] = s
			}
		}
		// git diff cannot see untracked files, but they are part of the
		// working tree a comparison against a ref should describe.
		if untracked, uerr := gitOutput(ctx, root, "ls-files", "--others", "--exclude-standard", "--", "*.go"); uerr == nil {
			for _, line := range strings.Split(string(untracked), "\n") {
				line = strings.TrimSuffix(line, "\r")
				if line == "" {
					continue
				}
				p := filepath.ToSlash(filepath.Clean(line))
				if _, ok := status[p]; !ok {
					status[p] = "A"
				}
			}
		}
	}

	changed := make(map[string]bool, len(status))
	paths := make([]string, 0, len(status))
	for p := range status {
		changed[p] = true
		paths = append(paths, p)
	}
	sort.Strings(paths)

	files := make([]JFileChange, 0, min(len(paths), maxChangeFiles))
	for _, p := range paths {
		if len(files) == maxChangeFiles {
			break
		}
		files = append(files, JFileChange{Path: p, Status: status[p]})
	}
	return changed, files, len(paths) - len(files), nil
}

func gitDiffStatus(ctx context.Context, root, rev string) ([]byte, error) {
	return gitOutput(ctx, root, "diff", "--name-status", "-M", rev, "--", "*.go")
}

// parseNameStatus maps a path to its first status letter. Renames and copies
// report the destination path, which is the file the head tree uses.
func parseNameStatus(out []byte) map[string]string {
	status := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		kind := strings.TrimSpace(fields[0])
		if kind == "" {
			continue
		}
		p := fields[len(fields)-1]
		if p == "" {
			continue
		}
		// git prints root-relative paths; normalize separators so a Windows
		// checkout still matches the loader's Rel values.
		status[filepath.ToSlash(filepath.Clean(p))] = kind[:1]
	}
	return status
}

// attachPatches reads the unified diff for the changed .go files and stores
// each file's block on its JFileChange. Worktree mode also synthesizes a
// new-file patch for untracked .go files, which git diff does not include.
// Every failure is non-fatal: the file keeps its status and an empty patch.
func attachPatches(root string, spec compareSpec, files []JFileChange) []JFileChange {
	if len(files) == 0 {
		return files
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	args := []string{"diff", "--no-color", "--unified=3", "-M"}
	switch spec.Mode {
	case compareRange:
		args = append(args, spec.Base, spec.To)
	case comparePR:
		args = append(args, spec.Base, spec.Head)
	default:
		// Base vs the working tree includes staged and unstaged edits. Start at
		// the merge-base, like the file list does, so base-branch drift does not
		// leak into the patch.
		from := spec.Base
		if mb, err := gitMergeBase(ctx, root, spec.Base, "HEAD"); err == nil {
			from = mb
		}
		args = append(args, from)
	}
	args = append(args, "--", "*.go")

	if out, err := gitOutput(ctx, root, args...); err == nil {
		patches := splitDiffPatches(out)
		for i := range files {
			p := filepath.ToSlash(filepath.Clean(files[i].Path))
			if patch, ok := patches[p]; ok {
				files[i].Patch = patch
			}
		}
	}

	if spec.Mode != compareWorktree {
		return files
	}

	untracked, err := gitOutput(ctx, root, "ls-files", "--others", "--exclude-standard", "--", "*.go")
	if err != nil {
		return files
	}
	for _, line := range strings.Split(string(untracked), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		p := filepath.ToSlash(filepath.Clean(line))
		body, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if rerr != nil {
			continue
		}
		patch := newFilePatch(p, body)
		for i := range files {
			if files[i].Status == "A" && filepath.ToSlash(filepath.Clean(files[i].Path)) == p {
				files[i].Patch = patch
				break
			}
		}
	}
	return files
}

// splitDiffPatches maps a file path to its `diff --git` block. The +++ line
// names the head side, so renames report the destination; a deletion has no
// +++ path and falls back to the --- line.
func splitDiffPatches(out []byte) map[string]string {
	patches := map[string]string{}
	var block []string
	flush := func() {
		if len(block) > 0 {
			if p := patchPath(block); p != "" {
				patches[p] = capPatch(strings.Join(block, "\n"))
			}
		}
		block = nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "diff --git ") && len(block) > 0 {
			flush()
		}
		block = append(block, line)
	}
	flush()
	return patches
}

func patchPath(block []string) string {
	var fallback string
	for _, line := range block {
		if strings.HasPrefix(line, "@@") {
			break
		}
		switch {
		case strings.HasPrefix(line, "+++ "):
			if p := patchHeaderPath(line, "+++ "); p != "" {
				return p
			}
		case strings.HasPrefix(line, "--- ") && fallback == "":
			fallback = patchHeaderPath(line, "--- ")
		}
	}
	return fallback
}

// patchHeaderPath strips the a/ or b/ marker and the C-style quoting git adds
// for unusual names. /dev/null (a create or delete) reports no path.
func patchHeaderPath(line, prefix string) string {
	p := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if p == "" || p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, `"`) {
		if unquoted, err := strconv.Unquote(p); err == nil {
			p = unquoted
		}
	}
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	return filepath.ToSlash(filepath.Clean(p))
}

func capPatch(patch string) string {
	if len(patch) <= maxPatchBytes {
		return patch
	}
	cut := maxPatchBytes
	for cut > 0 && !utf8.RuneStart(patch[cut]) {
		cut--
	}
	return patch[:cut] + patchTruncated
}

// newFilePatch renders an untracked file as a new-file diff block.
func newFilePatch(path string, body []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n", path, path, path)
	lines := strings.Split(string(body), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	fmt.Fprintf(&b, "@@ -0,0 +1,%d @@\n", len(lines))
	for _, line := range lines {
		b.WriteString("+")
		b.WriteString(strings.TrimSuffix(line, "\r"))
		b.WriteString("\n")
	}
	return capPatch(b.String())
}

// compareCommits lists the commits on the head side that the base side does not
// have. A working-tree comparison starts at the merge-base, like the file diff;
// uncommitted edits have no commit entry.
func compareCommits(root string, spec compareSpec) ([]JCommit, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	from, to := spec.Base, spec.Head
	if spec.Mode == compareWorktree {
		mb, err := gitMergeBase(ctx, root, spec.Base, "HEAD")
		if err != nil {
			return nil, 0, err
		}
		from, to = mb, "HEAD"
	}
	return recentCommits(ctx, root, from, to)
}

func gitMergeBase(ctx context.Context, root, a, b string) (string, error) {
	out, err := gitOutput(ctx, root, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func recentCommits(ctx context.Context, root, from, to string) ([]JCommit, int, error) {
	countOut, err := gitOutput(ctx, root, "rev-list", "--count", "--no-merges", from+".."+to)
	if err != nil {
		return nil, 0, err
	}
	total, err := strconv.Atoi(strings.TrimSpace(string(countOut)))
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	logOut, err := gitOutput(ctx, root, "log", "--no-merges", "-n", strconv.Itoa(maxChangeCommits),
		"--format=%H%x1f%an%x1f%aI%x1f%s", from+".."+to)
	if err != nil {
		return nil, 0, err
	}
	commits := parseCommitLog(logOut)
	more := total - len(commits)
	if more < 0 {
		more = 0
	}
	return commits, more, nil
}

// parseCommitLog reads the %H%x1f%an%x1f%aI%x1f%s format used by the compare
// view and the serve refs endpoint.
func parseCommitLog(out []byte) []JCommit {
	commits := make([]JCommit, 0, 8)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x1f", 4)
		if len(fields) != 4 {
			continue
		}
		commits = append(commits, JCommit{
			Sha: fields[0], Author: fields[1], Date: fields[2], Subject: fields[3],
		})
	}
	return commits
}

// markChanges flags the routes and stages whose file is in the changed set and
// records the comparison metadata. It answers "touches a file changed between
// the two sides", not "is a new route".
func markChanges(d *JData, changed map[string]bool, spec compareSpec, files []JFileChange, filesMore int, commits []JCommit, commitsMore int) {
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
	mode := string(spec.Mode)
	if mode == "" {
		mode = string(compareWorktree)
	}
	d.Change = &JChange{
		Ref:         spec.From,
		To:          spec.To,
		Mode:        mode,
		PR:          spec.PR,
		PRURL:       spec.PRURL,
		Files:       len(changed),
		Routes:      n,
		Commits:     commits,
		CommitsMore: commitsMore,
		FileList:    files,
		FilesMore:   filesMore,
	}
}
