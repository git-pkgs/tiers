package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func td(elem ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, elem...)...)
}

func execute(t *testing.T, argv ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	err = run(argv, &out, &errb)
	return out.String(), errb.String(), err
}

func TestListGo(t *testing.T) {
	out, _, err := execute(t, td("go", "leaf"), td("go", "mid"), td("go", "top"))
	if err != nil {
		t.Fatal(err)
	}
	want := "0  example.com/leaf\n1  example.com/mid\n2  example.com/top\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestListJSON(t *testing.T) {
	out, _, err := execute(t, "-json", td("go", "leaf"), td("go", "mid"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"Name": "example.com/leaf"`) {
		t.Errorf("json output missing leaf: %s", out)
	}
}

func TestUnknownEcosystemErrors(t *testing.T) {
	_, _, err := execute(t, "-ecosystem", "golnag", td("go", "leaf"))
	if err == nil || !strings.Contains(err.Error(), `"golnag" not present`) {
		t.Fatalf("expected unknown-ecosystem error, got %v", err)
	}
	if !strings.Contains(err.Error(), "golang") {
		t.Errorf("error should list available ecosystems: %v", err)
	}
}

func TestBumpUnknownEcosystemErrors(t *testing.T) {
	_, _, err := execute(t, "bump", "-ecosystem", "gme", "-set", "base=0.2.0", td("gem", "app"))
	if err == nil || !strings.Contains(err.Error(), `"gme" not present`) {
		t.Fatalf("expected unknown-ecosystem error, got %v", err)
	}
}

func TestListMultipleEcosystemsErrors(t *testing.T) {
	_, _, err := execute(t, td("go", "leaf"), td("gem", "base"))
	if err == nil || !strings.Contains(err.Error(), "multiple ecosystems") {
		t.Fatalf("expected multiple-ecosystems error, got %v", err)
	}
}

func TestListDot(t *testing.T) {
	out, _, err := execute(t, "-dot", td("go", "leaf"), td("go", "mid"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "digraph tiers") ||
		!strings.Contains(out, `"example.com/mid" -> "example.com/leaf"`) {
		t.Errorf("dot output = %q", out)
	}
}

func TestBumpGoDryRunCLI(t *testing.T) {
	out, _, err := execute(t, "bump", "-dry-run",
		"-set", "example.com/leaf=v0.2.0",
		td("go", "leaf"), td("go", "mid"))
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "dry  example.com/mid") {
		t.Errorf("missing dry status line:\n%s", out)
	}
	if !strings.Contains(out, "$ go get example.com/leaf@v0.2.0") {
		t.Errorf("missing go get command:\n%s", out)
	}
	if !strings.Contains(out, "$ go mod tidy") {
		t.Errorf("dry-run must show the then: chain (go mod tidy):\n%s", out)
	}
}

func TestBumpUnknownTargetCLI(t *testing.T) {
	_, _, err := execute(t, "bump", "-dry-run",
		"-set", "example.com/typo=v1.0.0",
		td("go", "leaf"), td("go", "mid"))
	if err == nil || !strings.Contains(err.Error(), "example.com/typo") {
		t.Fatalf("expected unknown-target error, got %v", err)
	}
}

func TestBumpNoDependentsMessage(t *testing.T) {
	// mid is a real package but nothing in {leaf, mid} depends on it.
	out, _, err := execute(t, "bump", "-dry-run",
		"-set", "example.com/mid=v0.2.0",
		td("go", "leaf"), td("go", "mid"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "nothing to bump") {
		t.Errorf("stdout = %q, want no-op message", out)
	}
}

func TestBumpUnsupportedEcosystemCLI(t *testing.T) {
	_, _, err := execute(t, "bump", "-dry-run", "-set", "clib=0.2.0",
		td("cargo", "lib"), td("cargo", "bin"))
	if err == nil || !strings.Contains(err.Error(), "cargo") {
		t.Fatalf("expected unsupported-ecosystem error, got %v", err)
	}
}

func TestWorkspaceMissingMemberCLI(t *testing.T) {
	_, _, err := execute(t, td("workspace-missing"))
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-member error, got %v", err)
	}
}

func TestMissingDirErrors(t *testing.T) {
	_, _, err := execute(t, td("go", "leaf"), td("does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should name the missing dir: %v", err)
	}
}

func TestVersion(t *testing.T) {
	for _, argv := range [][]string{{"version"}, {"-version"}, {"--version"}} {
		out, _, err := execute(t, argv...)
		if err != nil {
			t.Errorf("%v: err = %v, want nil", argv, err)
		}
		if !strings.HasPrefix(out, "tiers ") {
			t.Errorf("%v: stdout = %q", argv, out)
		}
	}
}

func TestHelpFlag(t *testing.T) {
	for _, argv := range [][]string{{"-h"}, {"--help"}, {"bump", "-h"}} {
		_, stderr, err := execute(t, argv...)
		if err != nil {
			t.Errorf("%v: err = %v, want nil", argv, err)
		}
		if !strings.Contains(stderr, "usage:") {
			t.Errorf("%v: stderr missing usage: %q", argv, stderr)
		}
	}
}

func TestBadFlagReturnsSilent(t *testing.T) {
	_, stderr, err := execute(t, "-nope")
	if !errors.Is(err, errSilent) {
		t.Fatalf("err = %v, want errSilent", err)
	}
	if !strings.Contains(stderr, "-nope") {
		t.Errorf("stderr should mention the bad flag: %q", stderr)
	}
}

func TestSubprocessHelpExitsZero(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}
	cmd := exec.Command("go", "run", ".", "-h")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("-h exited non-zero: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Errorf("stderr missing usage: %q", stderr.String())
	}
}

func TestSubprocessBadFlagPrintsOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}
	cmd := exec.Command("go", "run", ".", "-nope")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for bad flag")
	}
	// The flag package writes "flag provided but not defined: -nope" once.
	// main() must not repeat it.
	if n := strings.Count(stderr.String(), "-nope"); n != 1 {
		t.Errorf("flag error printed %d times, want 1:\n%s", n, stderr.String())
	}
}

func TestNoArgs(t *testing.T) {
	_, stderr, err := execute(t)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("stderr missing usage: %q", stderr)
	}
}
