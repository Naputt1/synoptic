package main

import (
	"os"
	"strings"
	"testing"
)

func corpusBase(routes, services, skipped, resolved, unresolved int) *corpusBaseline {
	return &corpusBaseline{Repos: map[string]corpusBaseEntry{
		"app": {
			corpusMetric: corpusMetric{
				Routes:     routes,
				Services:   services,
				Skipped:    skipped,
				Resolved:   resolved,
				Unresolved: unresolved,
			},
		},
	}}
}

func corpusResultFor(routes, services, skipped, resolved, unresolved int) corpusResult {
	return corpusResult{Name: "app", Metric: corpusMetric{
		Routes:     routes,
		Services:   services,
		Skipped:    skipped,
		Resolved:   resolved,
		Unresolved: unresolved,
	}}
}

func TestCorpusRouteDropFails(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 0), []corpusResult{corpusResultFor(19, 1, 0, 100, 0)})
	if len(diffs) != 1 || !strings.Contains(diffs[0].String(), "routes 20 -> 19") {
		t.Fatalf("diffs = %v, want one route drop", diffs)
	}
}

func TestCorpusRouteIncreasePasses(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 0), []corpusResult{corpusResultFor(25, 1, 0, 100, 0)})
	if len(diffs) != 0 {
		t.Fatalf("diffs = %v, want none", diffs)
	}
}

func TestCorpusSkipIncreaseFails(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 0), []corpusResult{corpusResultFor(20, 1, 1, 100, 0)})
	if len(diffs) != 1 || !strings.Contains(diffs[0].String(), "skipped 0 -> 1") {
		t.Fatalf("diffs = %v, want one skip increase", diffs)
	}
}

func TestCorpusUnresolvedIncreaseFails(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 2), []corpusResult{corpusResultFor(20, 1, 0, 100, 3)})
	if len(diffs) != 1 || !strings.Contains(diffs[0].String(), "unresolved 2 -> 3") {
		t.Fatalf("diffs = %v, want one unresolved increase", diffs)
	}
}

func TestCorpusResolutionDropWithinTolerancePasses(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 0), []corpusResult{corpusResultFor(20, 1, 0, 96, 0)})
	if len(diffs) != 0 {
		t.Fatalf("diffs = %v, want none for a 4%% drop", diffs)
	}

	diffs = compareCorpus(corpusBase(20, 1, 0, 100, 0), []corpusResult{corpusResultFor(20, 1, 0, 94, 0)})
	if len(diffs) != 1 || !strings.Contains(diffs[0].String(), "resolved 100 -> 94") {
		t.Fatalf("diffs = %v, want one resolved drop", diffs)
	}
}

func TestCorpusNewRepoIsReported(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 0), []corpusResult{
		corpusResultFor(20, 1, 0, 100, 0),
		{Name: "new", Metric: corpusMetric{Routes: 7}},
	})
	if len(diffs) != 1 || diffs[0].Repo != "new" || !strings.Contains(diffs[0].String(), "no baseline entry") {
		t.Fatalf("diffs = %v, want the new repo reported", diffs)
	}
}

func TestCorpusMissingPublicRepoFails(t *testing.T) {
	diffs := compareCorpus(corpusBase(20, 1, 0, 100, 0), nil)
	if len(diffs) != 1 || !strings.Contains(diffs[0].String(), "not measured") {
		t.Fatalf("diffs = %v, want the missing public repo reported", diffs)
	}
}

func TestCorpusMissingLocalRepoIsSkipped(t *testing.T) {
	base := corpusBase(20, 1, 0, 100, 0)
	entry := base.Repos["app"]
	entry.Local = true
	base.Repos["app"] = entry
	if diffs := compareCorpus(base, nil); len(diffs) != 0 {
		t.Fatalf("diffs = %v, want local entries skipped", diffs)
	}
}

func TestCorpusConfigRejectsShortSHA(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/corpus.json"
	body := `{"repos":[{"name":"x","url":"https://example.com/x","ref":"abc123","minRoutes":1}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCorpusConfig(path); err == nil || !strings.Contains(err.Error(), "40-char") {
		t.Fatalf("err = %v, want a full-SHA complaint", err)
	}
}
