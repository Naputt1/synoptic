package main

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// JDep is one Go package in the repo graph: its imports, the service entry
// points that reach it, and the files that make it up.
type JDep struct {
	P  string   `json:"p"`            // import path
	N  string   `json:"n"`            // package name
	D  string   `json:"d,omitempty"`  // directory relative to the repo root
	F  int      `json:"files"`        // files in the package
	Fn int      `json:"fn,omitempty"` // functions declared in the package
	I  []string `json:"i,omitempty"`  // in-repo imports
	X  []string `json:"x,omitempty"`  // third-party module paths
	By []string `json:"by,omitempty"` // services that reach this package
	Dr []string `json:"dr,omitempty"` // services that import it directly
	Fs []string `json:"fs,omitempty"` // files in the package
	M  bool     `json:"m,omitempty"`  // package main
	E  string   `json:"e,omitempty"`  // service name when M
}

// buildDeps groups the loaded files into packages and attributes each package
// to the entry points whose imports reach it. Everything is derived from the
// AST data already parsed by load.go; no package is guessed.
func buildDeps(m *Module) []JDep {
	byPath := map[string]*JDep{}
	for _, fc := range m.Files {
		d := byPath[fc.PkgPath]
		if d == nil {
			name := path.Base(fc.PkgPath)
			if fc.Ast != nil && fc.Ast.Name != nil {
				name = fc.Ast.Name.Name
			}
			dir := path.Dir(fc.Rel)
			if dir == "." {
				dir = ""
			}
			d = &JDep{P: fc.PkgPath, N: name, D: dir}
			byPath[fc.PkgPath] = d
		}
		d.F++
		d.Fs = append(d.Fs, fc.Rel)
	}
	for _, f := range m.Funcs {
		if d := byPath[f.PkgPath]; d != nil {
			d.Fn++
		}
	}

	mods := externalModules(m.Root)
	imported := map[string]map[string]bool{}
	usedExt := map[string]map[string]bool{}
	for _, fc := range m.Files {
		d := byPath[fc.PkgPath]
		if d == nil {
			continue
		}
		if imported[d.P] == nil {
			imported[d.P] = map[string]bool{}
			usedExt[d.P] = map[string]bool{}
		}
		for _, imp := range fc.Imports {
			if imp == d.P {
				continue
			}
			if _, ok := byPath[imp]; ok {
				if !imported[d.P][imp] {
					imported[d.P][imp] = true
					d.I = append(d.I, imp)
				}
				continue
			}
			mod := moduleFor(imp, mods)
			if mod != "" && !usedExt[d.P][mod] {
				usedExt[d.P][mod] = true
				d.X = append(d.X, mod)
			}
		}
	}

	byRel := map[string]*FileCtx{}
	for _, fc := range m.Files {
		byRel[fc.Rel] = fc
	}
	units := detectUnits(m)
	for _, u := range units {
		fc := byRel[u.entry]
		if fc == nil {
			continue
		}
		d := byPath[fc.PkgPath]
		if d == nil {
			continue
		}
		d.M = true
		if d.E != "" {
			d.E += ", "
		}
		d.E += u.name
	}

	// Reachability from each entry point names every package a service uses.
	// Direct imports are remembered separately from transitive ones.
	for _, u := range units {
		fc := byRel[u.entry]
		if fc == nil {
			continue
		}
		root := fc.PkgPath
		seen := map[string]bool{root: true}
		queue := []string{root}
		for len(queue) > 0 {
			p := queue[0]
			queue = queue[1:]
			d := byPath[p]
			if d == nil {
				continue
			}
			d.By = addName(d.By, u.name)
			for _, imp := range d.I {
				if seen[imp] {
					continue
				}
				seen[imp] = true
				queue = append(queue, imp)
			}
		}
		if rootDep := byPath[root]; rootDep != nil {
			for _, imp := range rootDep.I {
				if d := byPath[imp]; d != nil {
					d.Dr = addName(d.Dr, u.name)
				}
			}
		}
	}

	out := make([]JDep, 0, len(byPath))
	for _, d := range byPath {
		sort.Strings(d.I)
		sort.Strings(d.X)
		sort.Strings(d.By)
		sort.Strings(d.Dr)
		sort.Strings(d.Fs)
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].P < out[j].P })
	return out
}

func addName(list []string, name string) []string {
	for _, n := range list {
		if n == name {
			return list
		}
	}
	return append(list, name)
}

// externalModules reads every go.mod in the tree and returns the declared
// module paths, longest first, so an import path maps to the module that
// provides it instead of a heuristic when the module is known.
func externalModules(root string) []string {
	var mods []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		mods = append(mods, p)
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		inBlock := false
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if i := strings.Index(ln, "//"); i >= 0 {
				ln = strings.TrimSpace(ln[:i])
			}
			switch {
			case ln == "":
			case strings.HasPrefix(ln, "require ("):
				inBlock = true
			case inBlock && ln == ")":
				inBlock = false
			case inBlock:
				if f := strings.Fields(ln); len(f) > 0 {
					add(f[0])
				}
			case strings.HasPrefix(ln, "require "):
				if f := strings.Fields(strings.TrimPrefix(ln, "require")); len(f) > 0 {
					add(f[0])
				}
			}
		}
		return nil
	})
	sort.Slice(mods, func(i, j int) bool { return len(mods[i]) > len(mods[j]) })
	return mods
}

// moduleFor maps an import path to a third-party module. Standard library
// paths are skipped: they have no dot in their first segment.
func moduleFor(imp string, mods []string) string {
	segs := strings.Split(imp, "/")
	if len(segs) == 0 || !strings.Contains(segs[0], ".") || segs[0] == "C" {
		return ""
	}
	for _, mod := range mods {
		if imp == mod || strings.HasPrefix(imp, mod+"/") {
			return mod
		}
	}
	switch segs[0] {
	case "gopkg.in":
		if len(segs) >= 2 {
			return strings.Join(segs[:2], "/")
		}
	case "golang.org":
		if len(segs) >= 3 {
			return strings.Join(segs[:3], "/")
		}
	case "google.golang.org":
		if len(segs) >= 4 && segs[1] == "genproto" {
			return strings.Join(segs[:4], "/")
		}
		if len(segs) >= 2 {
			return strings.Join(segs[:2], "/")
		}
	case "go.uber.org", "k8s.io", "sigs.k8s.io":
		if len(segs) >= 2 {
			return strings.Join(segs[:2], "/")
		}
	case "cloud.google.com":
		if len(segs) >= 3 {
			return strings.Join(segs[:3], "/")
		}
	case "github.com", "gitlab.com", "bitbucket.org", "codeberg.org":
		if len(segs) >= 3 {
			return strings.Join(segs[:3], "/")
		}
	}
	if len(segs) >= 3 {
		return strings.Join(segs[:3], "/")
	}
	return strings.Join(segs[:2], "/")
}
