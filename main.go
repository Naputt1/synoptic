package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed ui/dist/index.html
var shellHTML string

// version is the release tag, set at build time with
// -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	root := flag.String("root", ".", "repository root to read")
	out := flag.String("out", "", "output html file (default <root>/synoptic.html)")
	js := flag.String("json", "", "also write the raw data as json; the report names the file")
	html := flag.Bool("html", true, "write the html page; -html=false prints the console report only")
	trace := flag.String("trace", "", "span export to overlay timings on (flat json spans or OTLP json)")
	typed := flag.Bool("typed", false, "type-check the repo to resolve calls the AST pass misses (slower; adds golang.org/x/tools; the repo must resolve its imports)")
	base := flag.String("base", "", "compare routes against this git ref: added, removed and modified, plus file-touch marks (best-effort; the tree is analyzed read-only)")
	to := flag.String("to", "", "compare -base against this git ref instead of the working tree; both sides are read-only worktrees")
	pr := flag.String("pr", "", "compare a GitHub pull request by number or URL; resolves its base and head with gh and uses their merge-base")
	openapi := flag.String("openapi", "", "write the proven route surface as OpenAPI 3.1 json; - means stdout")
	md := flag.String("md", "", "write the route inventory as markdown, for a PR comment; - means stdout")
	noSource := flag.Bool("no-source", false, "omit raw source from the page, keeping docs, files and line numbers")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("synoptic %s\n", version)
		return
	}

	rootAbs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}

	spec, hasCompare, note, err := compareFromFlags(rootAbs, *base, *to, *pr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
	if note != "" {
		fmt.Fprintf(os.Stderr, "compare: %s\n", note)
	}

	// A commit range or PR analyzes the head tree from a detached worktree, so
	// uncommitted edits are not part of the comparison. -base alone keeps
	// reading the working tree.
	headRoot := rootAbs
	if hasCompare && spec.Head != "" {
		var cleanup func()
		headRoot, cleanup, err = materializeRef(rootAbs, spec.Head)
		if err != nil {
			fmt.Fprintln(os.Stderr, "compare:", err)
			os.Exit(1)
		}
		defer cleanup()
	}

	m, err := loadModule(headRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	if *typed {
		n, terr := resolveTypedCalls(m, headRoot)
		fmt.Fprintf(os.Stderr, "typed: %d calls resolved\n", n)
		if terr != nil {
			// Type-checking is a best-effort enhancement: a broken dependency or
			// an offline module cache must not take the page down.
			fmt.Fprintf(os.Stderr, "typed: %v\n", terr)
		}
	}
	routes := buildRoutes(m)
	services := buildServices(m)
	if len(routes) == 0 && len(services) == 0 {
		fmt.Fprintln(os.Stderr, "no routes or RPC services found: the setup may use a constructor this build does not read yet")
		os.Exit(2)
	}
	d := buildData(m, routes)
	// The head tree may live in a temporary worktree; keep the name and paths
	// of the repository the user pointed at.
	d.Repo = baseName(rootAbs)
	attachServices(m, &d, services)
	if hasCompare {
		applyCompareView(&d, rootAbs, spec, os.Stderr)
	}
	if *trace != "" {
		tm, err := buildTiming(*trace, routes)
		if err != nil {
			fmt.Fprintln(os.Stderr, "trace:", err)
			os.Exit(1)
		}
		d.Timing = tm
	}
	if *noSource {
		redact(&d)
	}
	if *openapi != "" {
		blob, err := buildOpenAPI(d)
		if err != nil {
			fmt.Fprintln(os.Stderr, "openapi:", err)
			os.Exit(1)
		}
		if err := writeExport(*openapi, blob); err != nil {
			fmt.Fprintln(os.Stderr, "write openapi:", err)
			os.Exit(1)
		}
	}
	if *md != "" {
		if err := writeExport(*md, buildMarkdown(d)); err != nil {
			fmt.Fprintln(os.Stderr, "write md:", err)
			os.Exit(1)
		}
	}
	if *js != "" {
		if err := writeJSON(*js, d); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
			os.Exit(1)
		}
	}
	// A "-" export owns stdout; keep the human report on stderr so a pipe
	// still receives exactly the document.
	reportOut := os.Stdout
	if *openapi == "-" || *md == "-" {
		reportOut = os.Stderr
	}
	if !*html {
		report(reportOut, d, "", *js, *openapi, *md)
		return
	}
	if *out == "" {
		*out = filepath.Join(rootAbs, "synoptic.html")
	}
	if err := writeHTML(*out, d); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	report(reportOut, d, *out, *js, *openapi, *md)
}

// compareFromFlags validates the comparison flags and resolves a PR when one is
// requested. The bool reports whether any comparison was asked for.
func compareFromFlags(root, base, to, pr string) (compareSpec, bool, string, error) {
	if pr != "" {
		if base != "" || to != "" {
			return compareSpec{}, false, "", fmt.Errorf("-pr cannot be combined with -base or -to")
		}
		spec, note, err := resolvePR(root, pr)
		return spec, true, note, err
	}
	if to != "" {
		if base == "" {
			return compareSpec{}, false, "", fmt.Errorf("-to requires -base")
		}
		return rangeSpec(base, to), true, "", nil
	}
	if base != "" {
		return worktreeSpec(base), true, "", nil
	}
	return compareSpec{}, false, "", nil
}
