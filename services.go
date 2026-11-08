package main

import (
	"path"
	"sort"
	"strings"
)

// One service entry point: a package main inside the module. A monorepo keeps
// several of them side by side (jobs/cmd/main.go, users/cmd/main.go); their
// directories name the services.
type unit struct {
	name  string
	dir   string // forward-slash dir, "" when the entry point sits at the root
	entry string
}

// detectUnits returns every entry point as its own unit, sorted by name. The
// caller only splits the page when there is more than one.
func detectUnits(m *Module) []unit {
	byName := map[string]unit{}
	for _, f := range m.Funcs {
		if f.Name != "main" || f.Recv != "" || f.File == nil || f.File.Ast == nil {
			continue
		}
		if f.File.Ast.Name == nil || f.File.Ast.Name.Name != "main" {
			continue
		}
		dir := path.Dir(f.File.Rel)
		if dir == "." {
			dir = ""
		}
		if path.Base(dir) == "cmd" {
			dir = path.Dir(dir)
			if dir == "." {
				dir = ""
			}
		}
		name := path.Base(dir)
		if dir == "" || name == "." || name == "/" {
			name = m.Name
		}
		if name == "" {
			name = baseName(m.Root)
		}
		if _, dup := byName[name]; dup {
			continue
		}
		byName[name] = unit{name: name, dir: dir, entry: f.File.Rel}
	}
	out := make([]unit, 0, len(byName))
	for _, u := range byName {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// svcForFile picks the service whose directory owns a file. The longest
// matching directory wins, so a nested entry point beats the module root.
func svcForFile(svcs []JSvc, file string) string {
	best, bestLen := "", -1
	for _, s := range svcs {
		if s.D == "" {
			if bestLen < 0 {
				best, bestLen = s.N, 0
			}
			continue
		}
		if file == s.D || strings.HasPrefix(file, s.D+"/") {
			if len(s.D) > bestLen {
				best, bestLen = s.N, len(s.D)
			}
		}
	}
	return best
}
