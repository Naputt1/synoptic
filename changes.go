package main

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
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
	commits := make([]JCommit, 0, min(total, maxChangeCommits))
	for _, line := range strings.Split(string(logOut), "\n") {
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
	more := total - len(commits)
	if more < 0 {
		more = 0
	}
	return commits, more, nil
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
