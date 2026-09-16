package tiers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/git-pkgs/managers"
	"github.com/git-pkgs/managers/definitions"
)

// ecosystemManager maps a manifests ecosystem name to the managers
// definition that drives dependency updates for it.
var ecosystemManager = map[string]string{
	"golang": "gomod",
	"gem":    "bundler",
}

// brokenBump records ecosystems whose manager cannot apply the
// requested update to the manifest that discovery parsed.
var brokenBump = map[string]error{
	"gem": errors.New("bundler add writes to Gemfile, not the gemspec constraint; see https://github.com/git-pkgs/managers/issues/40"),
}

// ErrUnsupportedEcosystem is returned by Bump when the graph contains a
// package whose ecosystem has no manager mapping.
type ErrUnsupportedEcosystem struct {
	Ecosystem string
}

func (e *ErrUnsupportedEcosystem) Error() string {
	var available []string
	for k := range ecosystemManager {
		if brokenBump[k] == nil {
			available = append(available, k)
		}
	}
	if len(available) == 0 {
		return fmt.Sprintf("bump has no manager mapping for ecosystem %q and no ecosystem is currently available", e.Ecosystem)
	}
	sort.Strings(available)
	return fmt.Sprintf("bump has no manager mapping for ecosystem %q (available: %s)",
		e.Ecosystem, strings.Join(available, ", "))
}

// Bumper applies version bumps to packages in a Graph via the managers
// library.
type Bumper struct {
	detector *managers.Detector
	runner   managers.Runner
}

// NewBumper builds a Bumper backed by the given Runner. Pass
// managers.NewExecRunner() to actually execute commands, or
// managers.NewMockRunner() to capture them.
func NewBumper(runner managers.Runner) (*Bumper, error) {
	defs, err := definitions.LoadEmbedded()
	if err != nil {
		return nil, fmt.Errorf("loading manager definitions: %w", err)
	}
	translator := managers.NewTranslator()
	detector := managers.NewDetector(translator, runner)
	for _, def := range defs {
		detector.Register(def)
	}
	return &Bumper{detector: detector, runner: runner}, nil
}

// BumpResult records one attempted dependency bump.
type BumpResult struct {
	Package Package
	Dep     string
	Version string
	Manager string
	// Commands holds every command the manager ran for this bump, in
	// order: the primary command followed by any then: chain steps.
	Commands [][]string
	ExitCode int
	Stderr   string
	Err      error
}

// Bump updates each package's in-set dependencies to the versions given in
// targets. targets is keyed by dependency Name (e.g. "github.com/git-pkgs/purl"
// or "base"). A dependency without a target is left alone. Packages are
// visited in tier order so lower tiers are bumped before their dependents,
// though each Add operates only on that package's own directory.
func (b *Bumper) Bump(ctx context.Context, g *Graph, targets map[string]string) ([]BumpResult, error) {
	layers, err := g.Tiers()
	if err != nil {
		return nil, err
	}

	for _, eco := range g.Ecosystems() {
		if _, ok := ecosystemManager[eco]; !ok {
			return nil, &ErrUnsupportedEcosystem{Ecosystem: eco}
		}
	}

	names := map[string]bool{}
	for _, p := range g.Packages {
		names[p.Name] = true
	}
	var unknown []string
	for name := range targets {
		if !names[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown -set target(s): %s", strings.Join(unknown, ", "))
	}

	var results []BumpResult
	for _, tier := range layers {
		for _, p := range tier {
			mgrName := ecosystemManager[p.Ecosystem]
			for _, dep := range p.Deps {
				version, ok := targets[dep]
				if !ok {
					continue
				}
				results = append(results, b.one(ctx, p, mgrName, dep, version))
			}
		}
	}
	return results, nil
}

func (b *Bumper) one(ctx context.Context, p Package, mgrName, dep, version string) BumpResult {
	r := BumpResult{Package: p, Dep: dep, Version: version, Manager: mgrName}

	if err := brokenBump[p.Ecosystem]; err != nil {
		r.Err = err
		return r
	}

	mgr, err := b.detector.Detect(p.Dir, managers.DetectOptions{Manager: mgrName})
	if err != nil {
		r.Err = err
		return r
	}
	if !mgr.Supports(managers.CapAdd) {
		r.Err = fmt.Errorf("%s does not support add", mgrName)
		return r
	}

	res, err := mgr.Add(ctx, dep, managers.AddOptions{Version: version})
	if err != nil {
		r.Err = err
		return r
	}
	collect(&r, res)
	return r
}

// collect walks a Result and its Then chain, appending each command,
// accumulating stderr, and recording the first failure.
func collect(r *BumpResult, res *managers.Result) {
	r.Commands = append(r.Commands, res.Command)
	if s := strings.TrimSpace(res.Stderr); s != "" {
		if r.Stderr != "" {
			r.Stderr += "\n"
		}
		r.Stderr += s
	}
	if res.ExitCode != 0 && r.Err == nil {
		r.ExitCode = res.ExitCode
		r.Err = fmt.Errorf("%s: exit %d", strings.Join(res.Command, " "), res.ExitCode)
	}
	for _, then := range res.Then {
		collect(r, then)
	}
}
