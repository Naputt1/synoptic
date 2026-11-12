package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// The corpus is a pinned set of real repositories the analyzer is measured
// against. The config lives in testdata/corpus.json and the committed floor in
// testdata/corpus/baseline.json. This file holds the types and the comparison
// logic so they can be tested without the network or a clone.
//
// Keep this file in the normal build: corpus_compare_test.go runs in the
// ordinary go test path. Only the runner (corpus_test.go) is tagged.

type corpusConfig struct {
	Repos []corpusRepo `json:"repos"`
}

type corpusRepo struct {
	Name      string `json:"name"`
	URL       string `json:"url,omitempty"`
	Ref       string `json:"ref,omitempty"`   // full 40-char commit SHA
	Root      string `json:"root,omitempty"`  // module directory inside the clone
	Local     string `json:"local,omitempty"` // directory name under SYN_CORPUS_LOCAL
	MinRoutes int    `json:"minRoutes"`
	Framework string `json:"framework,omitempty"`
}

// corpusMetric is what one analyzer run contributes to the baseline. Counts
// only: no timings, paths, or host-specific data, so the file is stable.
type corpusMetric struct {
	Routes     int `json:"routes"`
	Services   int `json:"services"`
	Parsed     int `json:"parsed"`
	Skipped    int `json:"skipped"`
	Flows      int `json:"flows"`
	Resolved   int `json:"resolved"`
	Unresolved int `json:"unresolved"`
	Unknown    int `json:"unk"`
	External   int `json:"ext"`
	Truncated  int `json:"trunc"`
}

type corpusBaseline struct {
	Repos map[string]corpusBaseEntry `json:"repos"`
}

type corpusBaseEntry struct {
	corpusMetric
	Local     bool   `json:"local,omitempty"`
	Framework string `json:"framework,omitempty"`
}

type corpusResult struct {
	Name      string
	Framework string
	Local     bool
	Metric    corpusMetric
}

// corpusDiff is one regression. Baseline is the committed floor, Current is
// this run. An empty Field means the problem is structural (a repo appeared or
// disappeared), and Note carries the sentence.
type corpusDiff struct {
	Repo     string
	Field    string
	Baseline int
	Current  int
	Note     string
}

func (d corpusDiff) String() string {
	if d.Field == "" {
		return d.Repo + ": " + d.Note
	}
	s := fmt.Sprintf("%s: %s %d -> %d", d.Repo, d.Field, d.Baseline, d.Current)
	if d.Note != "" {
		s += " (" + d.Note + ")"
	}
	return s
}

// compareCorpus applies the tolerances:
//   - routes and services may not drop below the baseline,
//   - skipped and unresolved may not rise,
//   - resolved may drop at most 5%,
//   - a measured repo with no baseline entry is a failure, not a pass,
//   - a local baseline entry that was not measured is skipped (CI has no copies).
func compareCorpus(base *corpusBaseline, results []corpusResult) []corpusDiff {
	var diffs []corpusDiff
	seen := map[string]bool{}
	for _, r := range results {
		seen[r.Name] = true
		b, ok := base.Repos[r.Name]
		if !ok {
			diffs = append(diffs, corpusDiff{
				Repo:  r.Name,
				Note:  fmt.Sprintf("no baseline entry (measured %d routes); run UPDATE_CORPUS=1 and commit testdata/corpus/baseline.json", r.Metric.Routes),
				Field: "",
			})
			continue
		}
		if r.Metric.Routes < b.Routes {
			diffs = append(diffs, corpusDiff{Repo: r.Name, Field: "routes", Baseline: b.Routes, Current: r.Metric.Routes})
		}
		if b.Services > 0 && r.Metric.Services < b.Services {
			diffs = append(diffs, corpusDiff{Repo: r.Name, Field: "services", Baseline: b.Services, Current: r.Metric.Services})
		}
		if r.Metric.Skipped > b.Skipped {
			diffs = append(diffs, corpusDiff{Repo: r.Name, Field: "skipped", Baseline: b.Skipped, Current: r.Metric.Skipped})
		}
		if r.Metric.Unresolved > b.Unresolved {
			diffs = append(diffs, corpusDiff{Repo: r.Name, Field: "unresolved", Baseline: b.Unresolved, Current: r.Metric.Unresolved})
		}
		if b.Resolved > 0 && r.Metric.Resolved*100 < b.Resolved*95 {
			diffs = append(diffs, corpusDiff{
				Repo:     r.Name,
				Field:    "resolved",
				Baseline: b.Resolved,
				Current:  r.Metric.Resolved,
				Note:     "drop wider than 5%",
			})
		}
	}
	for name, b := range base.Repos {
		if seen[name] || b.Local {
			continue
		}
		diffs = append(diffs, corpusDiff{
			Repo:  name,
			Note:  "baseline entry was not measured (public repos must run on every corpus run)",
			Field: "",
		})
	}
	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Repo != diffs[j].Repo {
			return diffs[i].Repo < diffs[j].Repo
		}
		return diffs[i].Field < diffs[j].Field
	})
	return diffs
}

func loadCorpusConfig(path string) (corpusConfig, error) {
	var cfg corpusConfig
	blob, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if len(cfg.Repos) == 0 {
		return cfg, fmt.Errorf("%s: no repos", path)
	}
	seen := map[string]bool{}
	for i, r := range cfg.Repos {
		if r.Name == "" {
			return cfg, fmt.Errorf("%s: repo %d has no name", path, i)
		}
		if seen[r.Name] {
			return cfg, fmt.Errorf("%s: duplicate repo %q", path, r.Name)
		}
		seen[r.Name] = true
		if r.MinRoutes < 0 {
			return cfg, fmt.Errorf("%s: %s: minRoutes must not be negative", path, r.Name)
		}
		if r.Local == "" {
			if r.URL == "" {
				return cfg, fmt.Errorf("%s: %s: url is required for a public repo", path, r.Name)
			}
			if len(r.Ref) != 40 || strings.Trim(r.Ref, "0123456789abcdef") != "" {
				return cfg, fmt.Errorf("%s: %s: ref must be a full 40-char commit SHA", path, r.Name)
			}
		}
	}
	return cfg, nil
}

func loadCorpusBaseline(path string) (corpusBaseline, error) {
	base := corpusBaseline{Repos: map[string]corpusBaseEntry{}}
	blob, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return base, nil
	}
	if err != nil {
		return base, err
	}
	if err := json.Unmarshal(blob, &base); err != nil {
		return base, fmt.Errorf("%s: %w", path, err)
	}
	if base.Repos == nil {
		base.Repos = map[string]corpusBaseEntry{}
	}
	return base, nil
}

// baselineFrom keeps one entry per measured repo. Local repos are marked so a
// CI run can leave them out without failing.
func baselineFrom(results []corpusResult) corpusBaseline {
	base := corpusBaseline{Repos: map[string]corpusBaseEntry{}}
	for _, r := range results {
		base.Repos[r.Name] = corpusBaseEntry{
			corpusMetric: r.Metric,
			Local:        r.Local,
			Framework:    r.Framework,
		}
	}
	return base
}

func writeCorpusBaseline(path string, base corpusBaseline) error {
	blob, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o644)
}
