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

func main() {
	root := flag.String("root", ".", "repository root to read")
	out := flag.String("out", "", "output html file")
	js := flag.String("json", "", "also write the raw data as json (for the dev UI)")
	html := flag.Bool("html", true, "write the html page")
	trace := flag.String("trace", "", "span export to overlay timings on (flat json spans or OTLP json)")
	flag.Parse()

	m, err := loadModule(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	routes := buildRoutes(m)
	services := buildServices(m)
	if len(routes) == 0 && len(services) == 0 {
		fmt.Fprintln(os.Stderr, "no routes or RPC services found: the setup may use a constructor this build does not read yet")
		os.Exit(2)
	}
	d := buildData(m, routes)
	attachServices(m, &d, services)
	attachErrors(m, routes, &d)
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
