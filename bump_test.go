package tiers

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/git-pkgs/managers"
)

type capturingRunner struct {
	calls []struct {
		dir  string
		args []string
	}
}

func (r *capturingRunner) Run(ctx context.Context, dir string, args ...string) (*managers.Result, error) {
	r.calls = append(r.calls, struct {
		dir  string
		args []string
	}{dir: dir, args: args})
	return &managers.Result{Command: args, Cwd: dir, ExitCode: 0}, nil
}

func TestBumpGoCommand(t *testing.T) {
	g, err := Discover("testdata/go/leaf", "testdata/go/mid", "testdata/go/top")
	if err != nil {
		t.Fatal(err)
	}
	mock := managers.NewMockRunner()
	b, _ := NewBumper(mock)
	results, err := b.Bump(context.Background(), g, map[string]string{leaf: v020})
	if err != nil {
		t.Fatal(err)
	}
	// mid is tier 1, top is tier 2; mid must be bumped first.
	if results[0].Package.Name != mid || results[1].Package.Name != top {
		t.Errorf("order = %s, %s", results[0].Package.Name, results[1].Package.Name)
	}
	wantCmds := [][]string{
		{"go", "get", leaf + "@" + v020},
		{"go", "mod", "tidy"},
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s: err = %v", r.Package.Name, r.Err)
		}
		if r.Manager != "gomod" || r.Dep != leaf || r.Version != v020 {
			t.Errorf("result = %+v", r)
		}
		if !reflect.DeepEqual(r.Commands, wantCmds) {
			t.Errorf("%s commands = %v, want %v", r.Package.Name, r.Commands, wantCmds)
		}
	}
	// 2 bumps × (go get + go mod tidy) = 4 runner calls.
	if len(mock.Captured) != 4 {
		t.Fatalf("captured %d commands, want 4", len(mock.Captured))
	}
	if !reflect.DeepEqual(mock.Captured[0], wantCmds[0]) {
		t.Errorf("captured[0] = %v, want %v", mock.Captured[0], wantCmds[0])
	}
	if !reflect.DeepEqual(mock.Captured[1], wantCmds[1]) {
		t.Errorf("captured[1] = %v, want %v", mock.Captured[1], wantCmds[1])
	}
}

func TestCollectChain(t *testing.T) {
	res := &managers.Result{
		Command: []string{"go", "get", "x@v1"},
		Stderr:  "warn: get\n",
		Then: []*managers.Result{
			{Command: []string{"go", "mod", "tidy"}, ExitCode: 1, Stderr: "tidy failed"},
		},
	}
	var r BumpResult
	collect(&r, res)
	if len(r.Commands) != 2 {
		t.Fatalf("commands = %v", r.Commands)
	}
	if r.Err == nil || !strings.Contains(r.Err.Error(), "go mod tidy") || r.ExitCode != 1 {
		t.Errorf("err = %v, exit = %d; want tidy failure", r.Err, r.ExitCode)
	}
	if r.Stderr != "warn: get\ntidy failed" {
		t.Errorf("stderr = %q", r.Stderr)
	}
}

func TestBumpGemGuarded(t *testing.T) {
	g, err := Discover("testdata/gem/base", "testdata/gem/app")
	if err != nil {
		t.Fatal(err)
	}
	mock := managers.NewMockRunner()
	b, _ := NewBumper(mock)
	results, _ := b.Bump(context.Background(), g, map[string]string{base: "0.2.0"})
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if !errors.Is(results[0].Err, brokenBump[gem]) {
		t.Errorf("err = %v, want brokenBump[gem]", results[0].Err)
	}
	if len(mock.Captured) != 0 {
		t.Errorf("guard should not run commands, got %v", mock.Captured)
	}
}

func TestBumpWorkspaceCwd(t *testing.T) {
	g, err := Discover("testdata/workspace")
	if err != nil {
		t.Fatal(err)
	}
	runner := &capturingRunner{}
	b, _ := NewBumper(runner)
	_, err = b.Bump(context.Background(), g, map[string]string{
		"example.com/svc-a": v020,
	})
	if err != nil {
		t.Fatal(err)
	}
	// go get + go mod tidy, both in svc-b's directory.
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(runner.calls))
	}
	wantDir, _ := filepath.Abs("testdata/workspace/svc-b")
	for i, c := range runner.calls {
		if c.dir != wantDir {
			t.Errorf("call %d cwd = %q, want %q", i, c.dir, wantDir)
		}
	}
}

func TestBumpUnknownTarget(t *testing.T) {
	g, err := Discover("testdata/go/leaf", "testdata/go/mid")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewBumper(managers.NewMockRunner())
	_, err = b.Bump(context.Background(), g, map[string]string{"example.com/typo": v020})
	if err == nil || !strings.Contains(err.Error(), "example.com/typo") {
		t.Fatalf("expected unknown-target error, got %v", err)
	}
}

func TestBumpUnsupportedEcosystem(t *testing.T) {
	g, err := Discover("testdata/cargo/lib", "testdata/cargo/bin")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewBumper(managers.NewMockRunner())
	_, err = b.Bump(context.Background(), g, map[string]string{"clib": "0.2.0"})
	var ue *ErrUnsupportedEcosystem
	if !errors.As(err, &ue) {
		t.Fatalf("expected ErrUnsupportedEcosystem, got %v", err)
	}
	if ue.Ecosystem != "cargo" {
		t.Errorf("ecosystem = %q", ue.Ecosystem)
	}
}

func TestBumpSkipsUntargeted(t *testing.T) {
	g, err := Discover("testdata/go/leaf", "testdata/go/mid", "testdata/go/top")
	if err != nil {
		t.Fatal(err)
	}
	mock := managers.NewMockRunner()
	b, _ := NewBumper(mock)

	// Only target mid; leaf is not in targets so mid's dep on leaf is skipped.
	results, _ := b.Bump(context.Background(), g, map[string]string{
		mid: v020,
	})
	// top depends on leaf and mid; only mid is targeted -> 1 bump.
	if len(results) != 1 || results[0].Package.Name != top || results[0].Dep != mid {
		t.Fatalf("results = %+v", results)
	}
}
