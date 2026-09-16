package tiers

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	leaf   = "example.com/leaf"
	mid    = "example.com/mid"
	top    = "example.com/top"
	base   = "base"
	app    = "app"
	v020   = "v0.2.0"
	golang = "golang"
	gem    = "gem"
)

func names(tier []Package) []string {
	out := make([]string, len(tier))
	for i, p := range tier {
		out[i] = p.Name
	}
	return out
}

func TestDiscoverGo(t *testing.T) {
	g, err := Discover("testdata/go/leaf", "testdata/go/mid", "testdata/go/top")
	if err != nil {
		t.Fatal(err)
	}
	tiers, err := g.Tiers()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{leaf}, {mid}, {top}}
	if len(tiers) != len(want) {
		t.Fatalf("got %d tiers, want %d: %v", len(tiers), len(want), tiers)
	}
	for i := range want {
		if got := names(tiers[i]); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("tier %d = %v, want %v", i, got, want[i])
		}
	}

	var midPkg Package
	for _, p := range g.Packages {
		if p.Name == mid {
			midPkg = p
		}
	}
	if !reflect.DeepEqual(midPkg.Deps, []string{leaf}) {
		t.Errorf("mid deps = %v, want [%s]", midPkg.Deps, leaf)
	}
}

func TestDiscoverGem(t *testing.T) {
	g, err := Discover("testdata/gem/base", "testdata/gem/app")
	if err != nil {
		t.Fatal(err)
	}
	tiers, err := g.Tiers()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{base}, {app}}
	for i := range want {
		if got := names(tiers[i]); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("tier %d = %v, want %v", i, got, want[i])
		}
	}
	if tiers[0][0].Ecosystem != gem {
		t.Errorf("ecosystem = %q, want gem", tiers[0][0].Ecosystem)
	}
}

func TestWorkspaceMemberDirs(t *testing.T) {
	g, err := Discover("testdata/workspace")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Package{}
	for _, p := range g.Packages {
		byName[p.Name] = p
	}
	root, _ := filepath.Abs("testdata/workspace")
	cases := map[string]string{
		"example.com/svc-a": filepath.Join(root, "svc-a"),
		"example.com/svc-b": filepath.Join(root, "svc-b"),
	}
	for name, wantDir := range cases {
		p, ok := byName[name]
		if !ok {
			t.Fatalf("missing %s in %+v", name, g.Packages)
		}
		if p.Dir != wantDir {
			t.Errorf("%s Dir = %q, want %q", name, p.Dir, wantDir)
		}
		if p.Manifest != "go.mod" {
			t.Errorf("%s Manifest = %q, want go.mod", name, p.Manifest)
		}
	}
	if got := byName["example.com/svc-b"].Deps; !reflect.DeepEqual(got, []string{"example.com/svc-a"}) {
		t.Errorf("svc-b deps = %v", got)
	}
}

func TestWorkspaceMissingMember(t *testing.T) {
	_, err := Discover("testdata/workspace-missing")
	if err == nil {
		t.Fatal("expected error for missing workspace member")
	}
	if !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "go.work") {
		t.Errorf("error should name the missing member and go.work: %v", err)
	}
}

func TestDiscoverMissingDir(t *testing.T) {
	_, err := Discover("testdata/go/leaf", "testdata/does-not-exist")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscoverDuplicateIdentity(t *testing.T) {
	_, err := Discover("testdata/dup/one", "testdata/dup/two")
	var de *DuplicateError
	if !errors.As(err, &de) {
		t.Fatalf("expected DuplicateError, got %v", err)
	}
	if de.Name != "example.com/same" || de.Ecosystem != golang {
		t.Errorf("de = %+v", de)
	}
	if de.FirstDir == "" || de.SecondDir == "" || de.FirstDir == de.SecondDir {
		t.Errorf("dirs = %q, %q", de.FirstDir, de.SecondDir)
	}
}

func TestDuplicateErrorZeroValue(t *testing.T) {
	_ = (&DuplicateError{}).Error()
}

func TestDiscoverSameDirTwice(t *testing.T) {
	// Passing the same directory twice should not be a duplicate.
	g, err := Discover("testdata/go/leaf", "testdata/go/leaf")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Packages) != 1 {
		t.Errorf("packages = %d, want 1", len(g.Packages))
	}
}

func TestDiscoverNotADir(t *testing.T) {
	_, err := Discover("testdata/go/leaf/go.mod")
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}
}

func TestSkipEcosystems(t *testing.T) {
	// testdata/go/mid has a Dockerfile alongside go.mod.
	g, err := Discover("testdata/go/mid")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range g.Packages {
		if p.Ecosystem == "docker" {
			t.Errorf("docker package leaked: %+v", p)
		}
	}
	if got := g.Ecosystems(); !reflect.DeepEqual(got, []string{golang}) {
		t.Errorf("ecosystems = %v, want [golang]", got)
	}
}

func TestEcosystems(t *testing.T) {
	g, err := Discover(
		"testdata/go/leaf", "testdata/go/mid",
		"testdata/gem/base", "testdata/gem/app",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Ecosystems(); !reflect.DeepEqual(got, []string{gem, golang}) {
		t.Errorf("ecosystems = %v", got)
	}
}

func TestDiscoverMixed(t *testing.T) {
	g, err := Discover(
		"testdata/go/leaf", "testdata/go/mid", "testdata/go/top",
		"testdata/gem/base", "testdata/gem/app",
	)
	if err != nil {
		t.Fatal(err)
	}
	tiers, err := g.Tiers()
	if err != nil {
		t.Fatal(err)
	}
	// tier 0: base (gem), leaf (golang) — sorted by ecosystem then name
	if got := names(tiers[0]); !reflect.DeepEqual(got, []string{base, leaf}) {
		t.Errorf("tier 0 = %v", got)
	}
}

func TestFilter(t *testing.T) {
	g, err := Discover(
		"testdata/go/leaf", "testdata/go/mid",
		"testdata/gem/base", "testdata/gem/app",
	)
	if err != nil {
		t.Fatal(err)
	}
	filtered := g.Filter(golang)
	if len(filtered.Packages) != 2 {
		t.Errorf("golang packages = %d, want 2", len(filtered.Packages))
	}
	for _, p := range filtered.Packages {
		if p.Ecosystem != golang {
			t.Errorf("filter leaked %s", p.Ecosystem)
		}
	}
}

func TestCycle(t *testing.T) {
	g, err := Discover("testdata/cycle/a", "testdata/cycle/b")
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Tiers()
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CycleError, got %v", err)
	}
	got := names(ce.Members)
	if !reflect.DeepEqual(got, []string{"example.com/a", "example.com/b"}) {
		t.Errorf("cycle members = %v", got)
	}
}

func TestCycleExcludesDownstream(t *testing.T) {
	// a <-> b form a cycle; c depends on a but is not in the cycle.
	g, err := Discover("testdata/cycle/a", "testdata/cycle/b", "testdata/cycle/c")
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Tiers()
	var ce *CycleError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CycleError, got %v", err)
	}
	got := names(ce.Members)
	if !reflect.DeepEqual(got, []string{"example.com/a", "example.com/b"}) {
		t.Errorf("cycle members = %v, want [a b] (c should be excluded)", got)
	}
}

func TestDeterministic(t *testing.T) {
	dirs := []string{"testdata/go/leaf", "testdata/go/mid", "testdata/go/top"}
	g, _ := Discover(dirs...)
	first, _ := g.Tiers()
	for i := range 20 {
		g2, _ := Discover(dirs...)
		got, _ := g2.Tiers()
		if !reflect.DeepEqual(first, got) {
			t.Fatalf("run %d differs", i)
		}
	}
}
