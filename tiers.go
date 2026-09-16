// Package tiers computes a release order over a set of local package
// checkouts by parsing their manifests and layering the induced dependency
// graph. Tier 0 has no in-set dependencies; tier N depends only on tiers
// below N.
package tiers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/git-pkgs/manifests"
)

// Version is set at build time via ldflags.
var Version = "dev"

// Package is one publishable unit discovered from a manifest.
type Package struct {
	// Dir is the directory containing Manifest. For workspace members it
	// is the member directory, not the workspace root.
	Dir       string
	Ecosystem string
	Name      string
	// Manifest is the filename (no directory component) of the manifest
	// this package was parsed from, inside Dir.
	Manifest string
	// Deps holds the Names of other packages in the same graph that this
	// package depends on directly.
	Deps []string
}

func (p Package) key() key {
	return key{eco: p.Ecosystem, name: p.Name}
}

type key struct {
	eco  string
	name string
}

// Graph is the induced dependency graph over the input directories.
type Graph struct {
	Packages []Package
}

// Ecosystems returns the sorted set of ecosystems present in the graph.
func (g *Graph) Ecosystems() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range g.Packages {
		if !seen[p.Ecosystem] {
			seen[p.Ecosystem] = true
			out = append(out, p.Ecosystem)
		}
	}
	sort.Strings(out)
	return out
}

// DuplicateError is returned by Discover when two input directories
// declare the same package identity.
type DuplicateError struct {
	Ecosystem string
	Name      string
	FirstDir  string
	SecondDir string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("duplicate %s package %q in %s and %s",
		e.Ecosystem, e.Name, e.FirstDir, e.SecondDir)
}

// skipEcosystems are manifest formats that describe consumed tools or
// images rather than a publishable package identity, so they never form
// a node in the release graph.
var skipEcosystems = map[string]bool{
	"docker":         true,
	"github-actions": true,
	"git":            true,
	"pre-commit":     true,
	"asdf":           true,
	"brew":           true,
}

type raw struct {
	pkg  Package
	deps []manifests.Dependency
}

// Discover parses the manifests in each directory and returns the induced
// dependency graph over the resulting packages. A directory contributes one
// Package per manifest that declares its own name; a manifest with
// dependencies but no self-name (Gemfile, requirements.txt) contributes a
// Package named after the directory.
func Discover(dirs ...string) (*Graph, error) {
	var raws []raw
	byKey := map[key]int{}

	for _, dir := range dirs {
		found, err := scanDir(dir)
		if err != nil {
			return nil, err
		}
		for _, r := range found {
			if i, ok := byKey[r.pkg.key()]; ok {
				if raws[i].pkg.Dir != r.pkg.Dir {
					return nil, &DuplicateError{
						Ecosystem: r.pkg.Ecosystem,
						Name:      r.pkg.Name,
						FirstDir:  raws[i].pkg.Dir,
						SecondDir: r.pkg.Dir,
					}
				}
				raws[i].deps = append(raws[i].deps, r.deps...)
				continue
			}
			byKey[r.pkg.key()] = len(raws)
			raws = append(raws, r)
		}
	}

	g := &Graph{Packages: make([]Package, len(raws))}
	for i, r := range raws {
		r.pkg.Deps = filterDeps(r, byKey)
		g.Packages[i] = r.pkg
	}
	return g, nil
}

func scanDir(dir string) ([]raw, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", dir)
	}
	found, warns := manifests.DiscoverManifests(manifests.NewFSReader(os.DirFS(abs)))
	if len(warns) > 0 {
		return nil, fmt.Errorf("%s: %w", dir, errors.Join(warns...))
	}

	var out []raw
	for _, m := range found {
		if m.Kind != manifests.Manifest {
			continue
		}
		content, err := os.ReadFile(filepath.Join(abs, m.Path))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		res, err := manifests.Parse(m.Path, content)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", dir, m.Path, err)
		}
		if skipEcosystems[res.Ecosystem] {
			continue
		}
		pkgDir := filepath.Join(abs, filepath.Dir(m.Path))
		name := res.Name
		if name == "" {
			name = filepath.Base(pkgDir)
		}
		out = append(out, raw{
			pkg: Package{
				Dir:       pkgDir,
				Ecosystem: res.Ecosystem,
				Name:      name,
				Manifest:  filepath.Base(m.Path),
			},
			deps: res.Dependencies,
		})
	}
	return out, nil
}

func filterDeps(r raw, byKey map[key]int) []string {
	seen := map[string]bool{}
	var deps []string
	for _, d := range r.deps {
		if !d.Direct {
			continue
		}
		k := key{eco: r.pkg.Ecosystem, name: d.Name}
		if _, ok := byKey[k]; !ok || k == r.pkg.key() || seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		deps = append(deps, d.Name)
	}
	sort.Strings(deps)
	return deps
}

// Filter returns a graph containing only packages from the given ecosystem.
// Cross-ecosystem edges never exist, so Deps are already correct.
func (g *Graph) Filter(ecosystem string) *Graph {
	out := &Graph{}
	for _, p := range g.Packages {
		if p.Ecosystem == ecosystem {
			out.Packages = append(out.Packages, p)
		}
	}
	return out
}

// CycleError is returned by Tiers when the graph contains a dependency
// cycle. Members holds only the packages that participate in a cycle,
// not packages that merely depend on one.
type CycleError struct {
	Members []Package
}

func (e *CycleError) Error() string {
	names := make([]string, len(e.Members))
	for i, m := range e.Members {
		names[i] = m.Name
	}
	sort.Strings(names)
	return "dependency cycle: " + strings.Join(names, ", ")
}

// cycleMembers returns the packages that participate in a dependency
// cycle among the given remaining nodes, excluding packages that merely
// depend on a cycle without being part of one. Implemented as Tarjan's
// SCC over the remaining subgraph, keeping components of size > 1.
func cycleMembers(remaining map[key]Package) []Package {
	type state struct {
		index, low int
		onStack    bool
	}
	var (
		idx   int
		stack []key
		st    = map[key]*state{}
		out   []Package
	)
	var strong func(v key)
	strong = func(v key) {
		st[v] = &state{index: idx, low: idx, onStack: true}
		idx++
		stack = append(stack, v)
		for _, d := range remaining[v].Deps {
			w := key{eco: remaining[v].Ecosystem, name: d}
			if _, ok := remaining[w]; !ok {
				continue
			}
			if st[w] == nil {
				strong(w)
				st[v].low = min(st[v].low, st[w].low)
			} else if st[w].onStack {
				st[v].low = min(st[v].low, st[w].index)
			}
		}
		if st[v].low == st[v].index {
			var comp []Package
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				st[top].onStack = false
				comp = append(comp, remaining[top])
				if top == v {
					break
				}
			}
			if len(comp) > 1 {
				out = append(out, comp...)
			}
		}
	}
	for k := range remaining {
		if st[k] == nil {
			strong(k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Tiers layers the graph so that every package's in-set dependencies sit in
// a strictly lower tier. Packages within a tier are sorted by ecosystem then
// name.
func (g *Graph) Tiers() ([][]Package, error) {
	remaining := make(map[key]Package, len(g.Packages))
	deg := make(map[key]int, len(g.Packages))
	rev := make(map[key][]key, len(g.Packages))

	for _, p := range g.Packages {
		remaining[p.key()] = p
		deg[p.key()] = len(p.Deps)
		for _, d := range p.Deps {
			dk := key{eco: p.Ecosystem, name: d}
			rev[dk] = append(rev[dk], p.key())
		}
	}

	var tiers [][]Package
	for len(remaining) > 0 {
		var tier []Package
		for k, p := range remaining {
			if deg[k] == 0 {
				tier = append(tier, p)
			}
		}
		if len(tier) == 0 {
			return nil, &CycleError{Members: cycleMembers(remaining)}
		}
		sort.Slice(tier, func(i, j int) bool {
			if tier[i].Ecosystem != tier[j].Ecosystem {
				return tier[i].Ecosystem < tier[j].Ecosystem
			}
			return tier[i].Name < tier[j].Name
		})
		for _, p := range tier {
			delete(remaining, p.key())
			for _, dep := range rev[p.key()] {
				deg[dep]--
			}
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}
