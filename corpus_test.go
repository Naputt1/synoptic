//go:build corpus

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/tabwriter"
	"time"
)

const (
	corpusConfigPath   = "testdata/corpus.json"
	corpusBaselinePath = "testdata/corpus/baseline.json"
	corpusCloneDir     = ".corpus"
	corpusReportPath   = ".corpus/report.txt"
)

// TestCorpus measures the analyzer against pinned real repositories. It needs
// network access on a cold cache, so it is behind the corpus build tag and
// never runs in the ordinary go test ./... path or on pull requests.
//
//	UPDATE_CORPUS=1 go test -tags corpus -run TestCorpus -v .
//
// rewrites the committed baseline from this run; review that diff deliberately.
func TestCorpus(t *testing.T) {
	cfg, err := loadCorpusConfig(corpusConfigPath)
	if err != nil {
		t.Fatalf("corpus config: %v", err)
	}
	base, err := loadCorpusBaseline(corpusBaselinePath)
	if err != nil {
		t.Fatalf("corpus baseline: %v", err)
	}
	if _, err := os.Stat("ui/dist/index.html"); err != nil {
		t.Fatalf("ui/dist/index.html is missing: run `cd ui && bun install && bun run build` first")
	}

	bin := filepath.Join(t.TempDir(), "synoptic")
	if out, err := corpusCommand("go", "build", "-o", bin, "."); err != nil {
		t.Fatalf("build analyzer: %v\n%s", err, out)
	}

	localRoot := os.Getenv("SYN_CORPUS_LOCAL")
	if localRoot == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			t.Fatalf("home directory: %v", herr)
		}
		localRoot = filepath.Join(home, "git")
	}

	var (
		results []corpusResult
		rows    []corpusRow
		failed  []string
	)
	for _, repo := range cfg.Repos {
		var (
			root  string
			local bool
		)
		if repo.Local != "" {
			local = true
			root = filepath.Join(localRoot, repo.Local)
			if _, statErr := os.Stat(root); statErr != nil {
				t.Logf("corpus: skip local %s: not found at %s", repo.Name, root)
				continue
			}
		} else {
			clone, cloneErr := corpusClone(t, repo)
			if cloneErr != nil {
				rows = append(rows, corpusRow{Name: repo.Name, Repo: repo, Status: "clone failed"})
				failed = append(failed, fmt.Sprintf("%s: clone: %v", repo.Name, cloneErr))
				continue
			}
			root = clone
		}
		if repo.Root != "" {
			root = filepath.Join(root, repo.Root)
		}

		metric, runErr := corpusRun(bin, t.TempDir(), root)
		if runErr != nil {
			if local && corpusNoRoutes(runErr) {
				if _, ok := base.Repos[repo.Name]; !ok {
					t.Logf("corpus: skip local %s: analyzer found no routes", repo.Name)
					continue
				}
			}
			rows = append(rows, corpusRow{Name: repo.Name, Repo: repo, Status: "analyzer failed"})
			failed = append(failed, fmt.Sprintf("%s: analyzer: %v", repo.Name, runErr))
			continue
		}

		m := metric
		rows = append(rows, corpusRow{Name: repo.Name, Repo: repo, Metric: &m, Status: "ok"})
		if metric.Routes < repo.MinRoutes {
			status := fmt.Sprintf("routes %d below minRoutes %d", metric.Routes, repo.MinRoutes)
			rows[len(rows)-1].Status = status
			failed = append(failed, fmt.Sprintf("%s: %s", repo.Name, status))
		}
		results = append(results, corpusResult{
			Name:      repo.Name,
			Framework: repo.Framework,
			Local:     local,
			Metric:    metric,
		})
	}

	table := corpusTable(rows)
	t.Logf("corpus table\n%s", table)

	var diffs []corpusDiff
	if os.Getenv("UPDATE_CORPUS") != "" {
		if err := writeCorpusBaseline(corpusBaselinePath, baselineFrom(results)); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		t.Logf("corpus: baseline written to %s (%d repos)", corpusBaselinePath, len(results))
	} else {
		diffs = compareCorpus(&base, results)
		for _, d := range diffs {
			failed = append(failed, d.String())
		}
	}

	var report strings.Builder
	report.WriteString(table)
	if len(diffs) > 0 {
		report.WriteString("\nregressions:\n")
		for _, d := range diffs {
			report.WriteString("  " + d.String() + "\n")
		}
	}
	if len(failed) > 0 {
		report.WriteString("\nfailures:\n")
		for _, f := range failed {
			report.WriteString("  " + f + "\n")
		}
	}
	if err := os.MkdirAll(corpusCloneDir, 0o755); err == nil {
		if writeErr := os.WriteFile(corpusReportPath, []byte(report.String()), 0o644); writeErr != nil {
			t.Logf("corpus: write report: %v", writeErr)
		}
	} else {
		t.Logf("corpus: create report directory: %v", err)
	}

	for _, f := range failed {
		t.Errorf("%s", f)
	}
}

type corpusRow struct {
	Name   string
	Repo   corpusRepo
	Metric *corpusMetric
	Status string
}

func corpusTable(rows []corpusRow) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "repo\troutes\tmin\tsvcs\tparsed\tskip\tflows\tresolved\tblind\tunk\text\ttrunc\tresult")
	for _, r := range rows {
		if r.Metric == nil {
			fmt.Fprintf(w, "%s\t-\t%d\t-\t-\t-\t-\t-\t-\t-\t-\t-\t%s\n",
				r.Name, r.Repo.MinRoutes, r.Status)
			continue
		}
		m := r.Metric
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\n",
			r.Name, m.Routes, r.Repo.MinRoutes, m.Services, m.Parsed, m.Skipped,
			m.Flows, m.Resolved, m.Unresolved, m.Unknown, m.External, m.Truncated, r.Status)
	}
	w.Flush()
	return b.String()
}

func corpusClone(t *testing.T, repo corpusRepo) (string, error) {
	t.Helper()
	dir := filepath.Join(corpusCloneDir, repo.Name)
	if corpusHead(dir) == repo.Ref {
		return dir, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}

	// Fetching a specific commit keeps the clone shallow. GitHub accepts a
	// full SHA; a server that refuses one falls back to a full clone.
	if _, err := corpusCommand("git", "init", "-q", dir); err != nil {
		return "", fmt.Errorf("git init: %w", err)
	}
	if _, err := corpusCommand("git", "-C", dir, "remote", "add", "origin", repo.URL); err != nil {
		return "", fmt.Errorf("git remote: %w", err)
	}
	if _, err := corpusCommand("git", "-C", dir, "fetch", "--depth", "1", "origin", repo.Ref); err == nil {
		if out, err := corpusCommand("git", "-C", dir, "checkout", "-q", "--detach", "FETCH_HEAD"); err != nil {
			return "", fmt.Errorf("git checkout: %w\n%s", err, out)
		}
		return dir, nil
	} else {
		t.Logf("corpus: %s: shallow fetch of %s failed (%v), using a full clone", repo.Name, repo.Ref, err)
	}

	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if out, err := corpusCommand("git", "clone", "-q", repo.URL, dir); err != nil {
		return "", fmt.Errorf("git clone: %w\n%s", err, out)
	}
	if out, err := corpusCommand("git", "-C", dir, "checkout", "-q", "--detach", repo.Ref); err != nil {
		return "", fmt.Errorf("git checkout %s: %w\n%s", repo.Ref, err, out)
	}
	if got := corpusHead(dir); got != repo.Ref {
		return "", fmt.Errorf("clone is at %s, want %s", got, repo.Ref)
	}
	return dir, nil
}

func corpusHead(dir string) string {
	out, err := corpusCommand("git", "-C", dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func corpusRun(bin, tmp, root string) (corpusMetric, error) {
	outPath := filepath.Join(tmp, "out.json")
	out, err := corpusCommand(bin, "-root", root, "-json", outPath, "-html=false")
	if err != nil {
		return corpusMetric{}, fmt.Errorf("%w\n%s", err, out)
	}
	blob, err := os.ReadFile(outPath)
	if err != nil {
		return corpusMetric{}, fmt.Errorf("read analyzer json: %w", err)
	}
	var raw struct {
		Routes   []json.RawMessage          `json:"routes"`
		Services []json.RawMessage          `json:"services"`
		Flows    map[string]json.RawMessage `json:"flows"`
		Diag     struct {
			Parsed     int `json:"parsed"`
			Skipped    int `json:"skipped"`
			Resolved   int `json:"resolved"`
			Unresolved int `json:"unresolved"`
			Unknown    int `json:"unk"`
			External   int `json:"ext"`
			Truncated  int `json:"truncated"`
		} `json:"diag"`
	}
	if err := json.Unmarshal(blob, &raw); err != nil {
		return corpusMetric{}, fmt.Errorf("parse analyzer json: %w", err)
	}
	return corpusMetric{
		Routes:     len(raw.Routes),
		Services:   len(raw.Services),
		Parsed:     raw.Diag.Parsed,
		Skipped:    raw.Diag.Skipped,
		Flows:      len(raw.Flows),
		Resolved:   raw.Diag.Resolved,
		Unresolved: raw.Diag.Unresolved,
		Unknown:    raw.Diag.Unknown,
		External:   raw.Diag.External,
		Truncated:  raw.Diag.Truncated,
	}, nil
}

func corpusNoRoutes(err error) bool {
	return strings.Contains(err.Error(), "no routes or RPC services found")
}

func corpusCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > 4000 {
		text = text[len(text)-4000:]
	}
	return text, err
}
