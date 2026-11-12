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
	out := flag.String("out", "", "output html file")
	js := flag.String("json", "", "also write the raw data as json (for the dev UI)")
	html := flag.Bool("html", true, "write the html page")
	trace := flag.String("trace", "", "span export to overlay timings on (flat json spans or OTLP json)")
	typed := flag.Bool("typed", false, "use the Go type checker to resolve calls the AST pass misses (slower; the repo must type-check)")
	base := flag.String("base", "", "git ref to compare against: mark routes whose files changed since <ref> (best-effort)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("synoptic %s\n", version)
		return
	}

	m, err := loadModule(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	if *typed {
		n, terr := resolveTypedCalls(m, *root)
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
	attachServices(m, &d, services)
	if *base != "" {
		changed, err := changedFiles(m.Root, *base)
		if err != nil {
			// The change view is a convenience: a missing git, a bad ref or a
			// tree that is not a repo must not take the page down.
			fmt.Fprintf(os.Stderr, "base: %v (no change data)\n", err)
		} else {
			markChanges(&d, changed, *base)
		}
	}
	if *trace != "" {
		tm, err := buildTiming(*trace, routes)
		if err != nil {
			fmt.Fprintln(os.Stderr, "trace:", err)
			os.Exit(1)
		}
		d.Timing = tm
	}
	if *js != "" {
		if err := writeJSON(*js, d); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
			os.Exit(1)
		}
	}
	if !*html {
		report(d, *js)
		return
	}
	if *out == "" {
		*out = filepath.Join(m.Root, "synoptic.html")
	}
	if err := writeHTML(*out, d); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	report(d, *out)
}
