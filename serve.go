package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// maxServeCache bounds the successful reports kept in memory by -serve. A new
// key beyond the cap clears the cache; the analysis is cheap enough to repeat.
const maxServeCache = 16

// refsPayload is the data behind the comparison picker: the local refs, the
// recent commits, and the open pull requests when gh can list them.
type refsPayload struct {
	Current  string        `json:"current"`
	Branches []string      `json:"branches"`
	Tags     []string      `json:"tags"`
	Commits  []JCommit     `json:"commits"`
	PRs      []prListEntry `json:"prs"`
	PRsError string        `json:"prsError,omitempty"`
}

type prListEntry struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	URL         string `json:"url"`
}

type compareRequest struct {
	Base string `json:"base"`
	To   string `json:"to"`
}

type prRequest struct {
	PR string `json:"pr"`
}

// serveApp holds the working tree and the cache shared by a serve run's
// handlers.
type serveApp struct {
	root     string
	typed    bool
	noSource bool
	warn     io.Writer

	mu    sync.Mutex
	cache map[string][]byte
}

func newServeApp(root string, typed, noSource bool, warn io.Writer) *serveApp {
	if warn == nil {
		warn = io.Discard
	}
	return &serveApp{
		root:     root,
		typed:    typed,
		noSource: noSource,
		warn:     warn,
		cache:    map[string][]byte{},
	}
}

// handler wires the API and the embedded shell. Non-API paths other than / and
// /index.html return 404 so the page can detect that no static synoptic.json
// was generated beside it.
func (a *serveApp) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/initial", a.handleInitial)
	mux.HandleFunc("GET /api/refs", a.handleRefs)
	mux.HandleFunc("POST /api/compare", a.handleCompare)
	mux.HandleFunc("POST /api/pr", a.handlePR)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONBytes(w, []byte(`{"ok":true}`))
	})
	mux.HandleFunc("/", a.handleShell)
	return mux
}

func (a *serveApp) handleShell(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, shellHTML)
}

func (a *serveApp) handleInitial(w http.ResponseWriter, r *http.Request) {
	a.serveReport(w, "none|||", func() (JData, error) {
		d, _, err := buildReport(a.root, compareSpec{}, false, a.typed, a.noSource, io.Discard)
		return d, err
	})
}

func (a *serveApp) handleCompare(w http.ResponseWriter, r *http.Request) {
	var req compareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	base := strings.TrimSpace(req.Base)
	to := strings.TrimSpace(req.To)

	var spec compareSpec
	hasCompare := false
	key := "none|||"
	switch {
	case base == "" && to == "":
		// The working tree with no comparison.
	case base != "" && to == "":
		if !a.validRef(base) {
			http.Error(w, fmt.Sprintf("invalid base ref %q", base), http.StatusBadRequest)
			return
		}
		spec, hasCompare = worktreeSpec(base), true
		key = "worktree|" + base + "||"
	case base != "" && to != "":
		if !a.validRef(base) {
			http.Error(w, fmt.Sprintf("invalid base ref %q", base), http.StatusBadRequest)
			return
		}
		if !a.validRef(to) {
			http.Error(w, fmt.Sprintf("invalid to ref %q", to), http.StatusBadRequest)
			return
		}
		spec, hasCompare = rangeSpec(base, to), true
		key = "range|" + base + "|" + to + "|"
	default:
		http.Error(w, "to requires base", http.StatusBadRequest)
		return
	}

	a.serveReport(w, key, func() (JData, error) {
		d, _, err := buildReport(a.root, spec, hasCompare, a.typed, a.noSource, io.Discard)
		return d, err
	})
}

func (a *serveApp) handlePR(w http.ResponseWriter, r *http.Request) {
	var req prRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	arg := strings.TrimSpace(req.PR)
	if arg == "" {
		http.Error(w, "pr is required", http.StatusBadRequest)
		return
	}

	key := "pr|||" + arg
	if cached, ok := a.cached(key); ok {
		writeJSONBytes(w, cached)
		return
	}

	spec, note, err := resolvePR(a.root, arg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if note != "" {
		fmt.Fprintf(a.warn, "compare: %s\n", note)
	}
	d, _, err := buildReport(a.root, spec, true, a.typed, a.noSource, io.Discard)
	if err != nil {
		writeReportError(w, err)
		return
	}
	blob, err := json.Marshal(d)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.store(key, blob)
	writeJSONBytes(w, blob)
}

func (a *serveApp) handleRefs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	payload := refsPayload{
		Branches: []string{},
		Tags:     []string{},
		Commits:  []JCommit{},
		PRs:      []prListEntry{},
	}
	if out, err := gitOutput(ctx, a.root, "branch", "--show-current"); err == nil {
		payload.Current = strings.TrimSpace(string(out))
	}

	out, err := gitOutput(ctx, a.root, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		b := strings.TrimSpace(line)
		if b == "" || b == "origin/HEAD" || seen[b] {
			continue
		}
		seen[b] = true
		payload.Branches = append(payload.Branches, b)
	}
	sort.Strings(payload.Branches)

	if out, err := gitOutput(ctx, a.root, "for-each-ref", "--format=%(refname:short)", "refs/tags"); err == nil {
		seen = map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			payload.Tags = append(payload.Tags, t)
		}
		sort.Strings(payload.Tags)
	}

	if out, err := gitOutput(ctx, a.root, "log", "-n", "30", "--format=%H%x1f%an%x1f%aI%x1f%s", "HEAD"); err == nil {
		payload.Commits = parseCommitLog(out)
	}

	ghCtx, ghCancel := context.WithTimeout(r.Context(), 10*time.Second)
	out, err = runGH(ghCtx, a.root, "pr", "list", "--json",
		"number,title,headRefName,baseRefName,url", "--limit", "30")
	ghCancel()
	if err != nil {
		payload.PRsError = err.Error()
	} else if err := json.Unmarshal(out, &payload.PRs); err != nil {
		payload.PRsError = err.Error()
	}
	if payload.PRs == nil {
		payload.PRs = []prListEntry{}
	}

	blob, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONBytes(w, blob)
}

func (a *serveApp) validRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := gitOutput(ctx, a.root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// serveReport writes the cached JSON when present, otherwise builds the report
// and caches it. errNoRoutes is the caller's input problem, not a server fault.
func (a *serveApp) serveReport(w http.ResponseWriter, key string, build func() (JData, error)) {
	// Worktree comparisons depend on uncommitted edits, so they must never be
	// served from the cache. Committed ranges and PRs are immutable.
	cacheable := !strings.HasPrefix(key, "none") && !strings.HasPrefix(key, "worktree")
	if cacheable {
		if cached, ok := a.cached(key); ok {
			writeJSONBytes(w, cached)
			return
		}
	}
	d, err := build()
	if err != nil {
		writeReportError(w, err)
		return
	}
	blob, err := json.Marshal(d)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cacheable {
		a.store(key, blob)
	}
	writeJSONBytes(w, blob)
}

func writeReportError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, errNoRoutes) {
		code = http.StatusUnprocessableEntity
	}
	http.Error(w, err.Error(), code)
}

func writeJSONBytes(w http.ResponseWriter, blob []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(blob)
}

func (a *serveApp) cached(key string) ([]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	blob, ok := a.cache[key]
	return blob, ok
}

func (a *serveApp) store(key string, blob []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.cache[key]; !ok && len(a.cache) >= maxServeCache {
		a.cache = map[string][]byte{}
	}
	a.cache[key] = blob
}

// runServe starts the loopback server and blocks until SIGINT or SIGTERM.
func runServe(root, addr string, typed, noSource bool, warn io.Writer) error {
	if warn == nil {
		warn = io.Discard
	}
	listenAddr, err := loopbackAddr(addr)
	if err != nil {
		return err
	}

	app := newServeApp(root, typed, noSource, warn)
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           app.handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	fmt.Fprintf(warn, "synoptic serve: http://%s\n", listenAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-shutdownCtx.Done():
	}
	return nil
}

// loopbackAddr validates that the listen address is local and supplies the
// default host when the address is only a port.
func loopbackAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("addr %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
	default:
		return "", fmt.Errorf("addr %q: serve listens on loopback only", addr)
	}
	if port == "" {
		return "", fmt.Errorf("addr %q: missing port", addr)
	}
	return net.JoinHostPort(host, port), nil
}
