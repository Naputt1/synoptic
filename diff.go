package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// baseTimeout bounds the git work behind -base. A huge repository can take a
// moment to check out; the whole view is optional, so it must not hang a run.
const baseTimeout = 60 * time.Second

// materializeRef checks ref out into a detached temporary worktree and returns
// the tree to analyze for the base side of the diff.
//
// root may be a subdirectory of the repository; the returned baseRoot is the
// matching subdirectory inside the worktree, so the same relative paths and
// import graph line up with the working tree. cleanup removes the worktree and
// its temporary parent; it is safe to call once, and removes the directory even
// when git itself cannot.
func materializeRef(root, ref string) (baseRoot string, cleanup func(), err error) {
	ctx, cancel := context.WithTimeout(context.Background(), baseTimeout)
	defer cancel()

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	absRoot = realPath(absRoot)

	top, err := gitOutput(ctx, absRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, fmt.Errorf("not a git work tree: %w", err)
	}
	topDir := realPath(strings.TrimSpace(string(top)))

	rel, err := filepath.Rel(topDir, absRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("%s is outside the git work tree %s", absRoot, topDir)
	}

	parent, err := os.MkdirTemp("", "synoptic-base-")
	if err != nil {
		return "", nil, err
	}
	wt := filepath.Join(parent, "wt")
	cleanup = func() {
		rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rcancel()
		_ = exec.CommandContext(rctx, "git", "-C", topDir, "worktree", "remove", "--force", wt).Run()
		// The git command removes the tree; RemoveAll also drops the parent and
		// covers a worktree git no longer tracks.
		_ = os.RemoveAll(parent)
	}

	if _, err := gitOutput(ctx, topDir, "worktree", "add", "--detach", wt, ref); err != nil {
		_ = os.RemoveAll(parent)
		return "", nil, fmt.Errorf("materialize %s: %w", ref, err)
	}

	baseRoot = wt
	if rel != "." {
		baseRoot = filepath.Join(wt, rel)
	}
	if _, err := os.Stat(filepath.Join(baseRoot, "go.mod")); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("base %s has no go.mod under %s", ref, rel)
	}
	return baseRoot, cleanup, nil
}

// realPath resolves symlinks so a relative path computed from git's output
// matches the path Go was given (/var and /private/var on macOS).
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

// routeKey is the identity of a route across the two trees: method, path and
// service. svc is empty for a single-service repo.
func routeKey(r JRoute) string { return r.M + " " + r.P + " " + r.Svc }

func routeKeyOf(r JRoute) JRouteKey {
	return JRouteKey{M: r.M, P: r.P, Svc: r.Svc}
}

func lastStage(r JRoute) JStage {
	if len(r.Chain) == 0 {
		return JStage{}
	}
	return r.Chain[len(r.Chain)-1]
}

// middlewareSig is the names and order of every stage before the handler.
func middlewareSig(r JRoute) string {
	var names []string
	if len(r.Chain) > 0 {
		for _, st := range r.Chain[:len(r.Chain)-1] {
			names = append(names, st.Name)
		}
	}
	return strings.Join(names, "\x00")
}

// streamSig is the stream kind, or empty for a plain HTTP route. Pr and St.K
// carry the same fact; read both so a partially populated base still aligns.
func streamSig(r JRoute) string {
	if r.St != nil && r.St.K != "" {
		return r.St.K
	}
	return r.Pr
}

// routeWhy names what changed inside a route that kept its method, path and
// service. Docs are deliberately not a reason: rewording a comment is not a
// route change.
func routeWhy(base, head JRoute) []string {
	var why []string
	if middlewareSig(base) != middlewareSig(head) {
		why = append(why, "middleware")
	}
	b, h := lastStage(base), lastStage(head)
	if b.Name != h.Name || b.K != h.K {
		why = append(why, "handler")
	}
	if streamSig(base) != streamSig(head) {
		why = append(why, "stream")
	}
	return why
}

// diffRoutes compares the route sets and returns the base view. Routes keep
// their identity through routeKey, so a handler or middleware edit becomes
// "modified" instead of a remove plus an add.
func diffRoutes(base, head JData) *JChange {
	c := &JChange{}
	old := make(map[string]JRoute, len(base.Routes))
	for _, r := range base.Routes {
		old[routeKey(r)] = r
	}
	seen := make(map[string]bool, len(head.Routes))
	for _, r := range head.Routes {
		k := routeKey(r)
		seen[k] = true
		b, ok := old[k]
		if !ok {
			c.Added = append(c.Added, routeKeyOf(r))
			continue
		}
		if why := routeWhy(b, r); len(why) > 0 {
			c.Modified = append(c.Modified, JRouteMod{
				M: r.M, P: r.P, Svc: r.Svc, Why: why,
			})
		}
	}
	for _, r := range base.Routes {
		if !seen[routeKey(r)] {
			c.Removed = append(c.Removed, routeKeyOf(r))
		}
	}
	c.Routes = len(c.Added) + len(c.Modified)
	return c
}

// applyRouteDiff folds the route diff into the head data. It sets New and Chg
// on added routes, Chg on modified ones, and Chg on the stages that differ from
// the base chain at the same position. File-touch marks are left in place.
func applyRouteDiff(head *JData, base JData) {
	diff := diffRoutes(base, *head)
	added := make(map[string]bool, len(diff.Added))
	for _, r := range diff.Added {
		added[r.M+" "+r.P+" "+r.Svc] = true
	}
	old := make(map[string]JRoute, len(base.Routes))
	for _, r := range base.Routes {
		old[routeKey(r)] = r
	}
	for i := range head.Routes {
		r := &head.Routes[i]
		k := routeKey(*r)
		if added[k] {
			r.New = true
			r.Chg = true
			continue
		}
		b, ok := old[k]
		if !ok || len(routeWhy(b, *r)) == 0 {
			continue
		}
		r.Chg = true
		for j := range r.Chain {
			if j >= len(b.Chain) || b.Chain[j].Name != r.Chain[j].Name || b.Chain[j].K != r.Chain[j].K {
				r.Chain[j].Chg = true
			}
		}
	}
	if head.Change == nil {
		head.Change = diff
		return
	}
	head.Change.Added = diff.Added
	head.Change.Removed = diff.Removed
	head.Change.Modified = diff.Modified
	head.Change.Routes = diff.Routes
}

// analyzeRoot runs the head pipeline over a tree without writing anything. It
// is used for the base worktree; typed resolution is never run there.
func analyzeRoot(root string) (JData, error) {
	m, err := loadModule(root)
	if err != nil {
		return JData{}, err
	}
	d := buildData(m, buildRoutes(m))
	attachServices(m, &d, buildServices(m))
	return d, nil
}

// applyBaseView adds the -base view to d. It marks file-touch changes first,
// then, when the base tree can be materialized and analyzed, replaces the
// counters with a real route diff. Every failure is a warning: the current
// tree still describes itself, so the run continues with the file view.
func applyBaseView(d *JData, root, ref string, warn io.Writer) {
	changed, err := changedFiles(root, ref)
	if err != nil {
		fmt.Fprintf(warn, "base: %v (no change data)\n", err)
		return
	}
	markChanges(d, changed, ref)

	baseRoot, cleanup, err := materializeRef(root, ref)
	if err != nil {
		fmt.Fprintf(warn, "base: %v (file view only)\n", err)
		return
	}
	defer cleanup()

	base, err := analyzeRoot(baseRoot)
	if err != nil {
		fmt.Fprintf(warn, "base: %v (file view only)\n", err)
		return
	}
	applyRouteDiff(d, base)
}
