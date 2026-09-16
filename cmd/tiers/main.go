package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/git-pkgs/managers"
	"github.com/git-pkgs/tiers"
)

// errSilent signals a non-zero exit where the message has already been
// written to stderr (e.g. by flag.FlagSet on a parse failure).
var errSilent = errors.New("silent exit")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintln(os.Stderr, "tiers:", err)
		}
		os.Exit(1)
	}
}

func run(argv []string, stdout, stderr io.Writer) error {
	if len(argv) > 0 {
		switch argv[0] {
		case "version", "-version", "--version":
			fmt.Fprintln(stdout, "tiers", tiers.Version)
			return nil
		case "bump":
			return runBump(argv[1:], stdout, stderr)
		}
	}
	return runList(argv, stdout, stderr)
}

type setFlag map[string]string

func (s setFlag) String() string { return "" }
func (s setFlag) Set(v string) error {
	name, ver, ok := strings.Cut(v, "=")
	if !ok || name == "" || ver == "" {
		return fmt.Errorf("want name=version, got %q", v)
	}
	s[name] = ver
	return nil
}

func selectEcosystem(g *tiers.Graph, eco string) (*tiers.Graph, error) {
	found := g.Ecosystems()
	if len(found) == 0 {
		return nil, errors.New("no manifests found")
	}
	if eco != "" {
		if slices.Contains(found, eco) {
			return g.Filter(eco), nil
		}
		return nil, fmt.Errorf("ecosystem %q not present; found: %s", eco, strings.Join(found, ", "))
	}
	if len(found) == 1 {
		return g.Filter(found[0]), nil
	}
	return nil, fmt.Errorf("multiple ecosystems found (%s); pick one with -ecosystem",
		strings.Join(found, ", "))
}

func runBump(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targets := setFlag{}
	fs.Var(&targets, "set", "target version for a package (name=version, repeatable)")
	eco := fs.String("ecosystem", "", "the ecosystem to operate on (auto-detected when only one is present)")
	dry := fs.Bool("dry-run", false, "print commands without running them")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tiers bump -set <name>=<version> [-set ...] [flags] <dir>...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errSilent
	}
	dirs := fs.Args()
	if len(dirs) == 0 || len(targets) == 0 {
		fs.Usage()
		return errors.New("need at least one directory and one -set")
	}

	g, err := tiers.Discover(dirs...)
	if err != nil {
		return err
	}
	g, err = selectEcosystem(g, *eco)
	if err != nil {
		return err
	}

	var runner managers.Runner = managers.NewExecRunner()
	if *dry {
		runner = managers.NewMockRunner()
	}
	b, err := tiers.NewBumper(runner)
	if err != nil {
		return err
	}

	results, err := b.Bump(context.Background(), g, targets)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Fprintln(stdout, "nothing to bump: no package in the graph depends on the given targets")
		return nil
	}

	failed := 0
	for _, r := range results {
		status := "ok"
		if *dry {
			status = "dry"
		}
		if r.Err != nil {
			status = "FAIL"
			failed++
		}
		fmt.Fprintf(stdout, "%-4s %-40s %s@%s\n", status, r.Package.Name, r.Dep, r.Version)
		for _, cmd := range r.Commands {
			fmt.Fprintf(stdout, "     $ %s\n", strings.Join(cmd, " "))
		}
		if r.Stderr != "" {
			for line := range strings.SplitSeq(r.Stderr, "\n") {
				fmt.Fprintf(stdout, "     %s\n", line)
			}
		}
		if r.Err != nil {
			fmt.Fprintf(stdout, "     %v\n", r.Err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d bump(s) failed", failed)
	}
	return nil
}

func runList(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("tiers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		eco     = fs.String("ecosystem", "", "the ecosystem to operate on (golang, gem, npm, ...); auto-detected when only one is present")
		asJSON  = fs.Bool("json", false, "emit tiers as JSON")
		asDot   = fs.Bool("dot", false, "emit the induced graph as graphviz dot")
		verbose = fs.Bool("v", false, "print each package's directory and in-set deps")
	)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: tiers [flags] <dir>...")
		fmt.Fprintln(stderr, "       tiers bump -set <name>=<version> [flags] <dir>...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errSilent
	}

	dirs := fs.Args()
	if len(dirs) == 0 {
		fs.Usage()
		return errors.New("no directories given")
	}

	g, err := tiers.Discover(dirs...)
	if err != nil {
		return err
	}
	g, err = selectEcosystem(g, *eco)
	if err != nil {
		return err
	}

	if *asDot {
		return writeDot(stdout, g)
	}

	layers, err := g.Tiers()
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(layers)
	}

	for i, tier := range layers {
		if *verbose {
			printVerboseTier(stdout, i, tier)
			continue
		}
		names := make([]string, len(tier))
		for j, p := range tier {
			names[j] = p.Name
		}
		fmt.Fprintf(stdout, "%d  %s\n", i, strings.Join(names, "  "))
	}
	return nil
}

const nameCol = 40

func printVerboseTier(w io.Writer, i int, tier []tiers.Package) {
	fmt.Fprintf(w, "tier %d\n", i)
	for _, p := range tier {
		fmt.Fprintf(w, "  %-8s %-*s %s\n", p.Ecosystem, nameCol, p.Name, p.Dir)
		for _, d := range p.Deps {
			fmt.Fprintf(w, "           %*s <- %s\n", nameCol, "", d)
		}
	}
}

func writeDot(w io.Writer, g *tiers.Graph) error {
	fmt.Fprintln(w, "digraph tiers {")
	fmt.Fprintln(w, "  rankdir=BT;")
	for _, p := range g.Packages {
		fmt.Fprintf(w, "  %q;\n", p.Name)
		for _, d := range p.Deps {
			fmt.Fprintf(w, "  %q -> %q;\n", p.Name, d)
		}
	}
	fmt.Fprintln(w, "}")
	return nil
}
