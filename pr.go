package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// prInfo is the subset of `gh pr view --json` that resolves the two sides of a
// pull request comparison.
type prInfo struct {
	Number      int    `json:"number"`
	BaseRefName string `json:"baseRefName"`
	BaseRefOid  string `json:"baseRefOid"`
	HeadRefOid  string `json:"headRefOid"`
	URL         string `json:"url"`
}

// resolvePR turns a pull request number or URL into a comparison between its
// merge-base and its head commit.
//
// It is the only path that may touch the network: when the PR head or base
// commit is not present locally, it fetches the single ref it needs. The
// returned note describes any fetch and is printed by main.
func resolvePR(root, arg string) (compareSpec, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := runGH(ctx, root, "pr", "view", arg, "--json",
		"number,baseRefName,baseRefOid,headRefOid,url")
	if err != nil {
		return compareSpec{}, "", fmt.Errorf("gh pr view %s: %w (use -base <ref> -to <ref> when gh is unavailable)", arg, err)
	}
	var info prInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return compareSpec{}, "", fmt.Errorf("gh pr view %s: %w", arg, err)
	}
	if info.Number == 0 || info.HeadRefOid == "" {
		return compareSpec{}, "", fmt.Errorf("gh pr view %s: no pull request head", arg)
	}

	var notes []string

	// The PR's base tip is the source of truth for the merge-base; a local
	// origin/<branch> may be stale. Fall back to the remote ref, then to one
	// fetch when neither commit is present.
	origin := "origin/" + info.BaseRefName
	baseRef := ""
	switch {
	case info.BaseRefOid != "" && commitExists(ctx, root, info.BaseRefOid):
		baseRef = info.BaseRefOid
	case info.BaseRefName != "" && commitExists(ctx, root, origin):
		baseRef = origin
	case info.BaseRefName != "":
		if _, ferr := gitOutput(ctx, root, "fetch", "--no-tags", "origin", info.BaseRefName); ferr == nil {
			notes = append(notes, "fetched "+info.BaseRefName)
			switch {
			case info.BaseRefOid != "" && commitExists(ctx, root, info.BaseRefOid):
				baseRef = info.BaseRefOid
			case commitExists(ctx, root, origin):
				baseRef = origin
			default:
				if fetched, rerr := gitOutput(ctx, root, "rev-parse", "FETCH_HEAD"); rerr == nil {
					baseRef = strings.TrimSpace(string(fetched))
				}
			}
		}
	}
	if baseRef == "" {
		return compareSpec{}, "", fmt.Errorf("pr %d: base branch %q is not available locally", info.Number, info.BaseRefName)
	}

	headRef := info.HeadRefOid
	if !commitExists(ctx, root, headRef) {
		prRef := "refs/pull/" + strconv.Itoa(info.Number) + "/head"
		if _, ferr := gitOutput(ctx, root, "fetch", "--no-tags", "origin", prRef); ferr != nil {
			return compareSpec{}, "", fmt.Errorf("pr %d: head %s is not available locally and could not be fetched: %w",
				info.Number, shortSha(headRef), ferr)
		}
		fetched, rerr := gitOutput(ctx, root, "rev-parse", "FETCH_HEAD")
		if rerr != nil {
			return compareSpec{}, "", fmt.Errorf("pr %d: read fetched head: %w", info.Number, rerr)
		}
		headRef = strings.TrimSpace(string(fetched))
		notes = append(notes, "fetched "+prRef)
	}

	// The merge-base is the PR's own change set. A shallow clone may not have
	// the history to find it; fall back to the base tip and say so.
	baseCommit := baseRef
	if mb, merr := gitMergeBase(ctx, root, baseRef, headRef); merr == nil {
		baseCommit = mb
	} else {
		notes = append(notes, "merge-base unavailable; comparing against "+baseRef)
	}

	from := shortSha(baseRef)
	if info.BaseRefName != "" && commitExists(ctx, root, origin) {
		from = origin
	}
	return compareSpec{
		Mode:  comparePR,
		From:  from,
		To:    headRef,
		Base:  baseCommit,
		Head:  headRef,
		PR:    info.Number,
		PRURL: info.URL,
	}, strings.Join(notes, "; "), nil
}

func runGH(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
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

func commitExists(ctx context.Context, root, rev string) bool {
	if rev == "" {
		return false
	}
	_, err := gitOutput(ctx, root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return err == nil
}
