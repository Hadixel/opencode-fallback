package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsLimited(t *testing.T) {
	for _, s := range []string{
		"Error: Free usage limit reached. Subscribe to OpenCode Go to keep going: https://opencode.ai/go",
		`{"type":"error","error":{"type":"FreeTierError","message":"OpenCode's free tier can only be used from within OpenCode."}}`,
		"HTTP 429 Too Many Requests",
		"rate limit reached, retry later",
	} {
		if !isLimited(s) {
			t.Errorf("isLimited(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"ok",
		"Error: no active credentials for provider: oc-zenfree",
		"the build finished in 429ms",
		"quarterly report",
	} {
		if isLimited(s) {
			t.Errorf("isLimited(%q) = true, want false", s)
		}
	}
}

func TestIsUnavailable(t *testing.T) {
	for _, s := range []string{
		`{"error":{"message":"No active credentials for provider: oc-zenfree","code":"model_not_found"}}`,
		"Error: model not found: foo",
		"unknown model bar",
	} {
		if !isUnavailable(s) {
			t.Errorf("isUnavailable(%q) = false, want true", s)
		}
	}
	if isUnavailable("Free usage limit reached") {
		t.Error("a quota is not an unavailable model")
	}
}

func TestSplitChain(t *testing.T) {
	got := splitChain(" a/x , ,b/y,")
	want := []string{"a/x", "b/y"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("splitChain = %v, want %v", got, want)
	}
	if got := splitChain("  "); len(got) != 0 {
		t.Fatalf("splitChain(blank) = %v, want empty", got)
	}
}

// fakeOpencode installs a stub opencode on PATH. $3 is the model id.
func fakeOpencode(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$3" in
  a/limited) echo "Error: Free usage limit reached. Subscribe to OpenCode Go to keep going"; exit 0 ;;
  a/missing) echo '{"error":{"message":"No active credentials for provider: a","code":"model_not_found"}}'; exit 0 ;;
  *) echo ok; exit 0 ;;
esac
`
	path := filepath.Join(dir, "opencode")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func capture(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	oldOut, oldErr := stdout, stderr
	stdout, stderr = &out, &errb
	t.Cleanup(func() {
		stdout, stderr = oldOut, oldErr
	})
	return &out, &errb
}

func TestRunFallsBack(t *testing.T) {
	fakeOpencode(t)
	out, errb := capture(t)

	code := runCmd([]string{"--chain", "a/limited,a/missing,a/ok", "say ok"})
	if code != 0 {
		t.Fatalf("runCmd = %d, want 0; stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("answer not passed through: %q", out.String())
	}
	for _, want := range []string{"rate-limited", "unavailable"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("stderr = %q, want %q banner", errb.String(), want)
		}
	}
}

func TestRunAllLimited(t *testing.T) {
	fakeOpencode(t)
	_, errb := capture(t)

	code := runCmd([]string{"--chain", "a/limited", "say ok"})
	if code != 3 {
		t.Fatalf("runCmd = %d, want 3; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "per source IP") {
		t.Errorf("the all-limited message should name the real cause: %q", errb.String())
	}
}

func TestCheckReports(t *testing.T) {
	fakeOpencode(t)
	out, _ := capture(t)
	old := egressIPFn
	egressIPFn = func(timeout time.Duration) (string, error) { return "1.2.3.4", nil }
	t.Cleanup(func() { egressIPFn = old })

	code := checkCmd([]string{"--chain", "a/ok,a/limited"})
	if code != 3 {
		t.Fatalf("checkCmd = %d, want 3", code)
	}
	if !strings.Contains(out.String(), "egress: 1.2.3.4") {
		t.Errorf("missing egress line: %q", out.String())
	}
	if !strings.Contains(out.String(), "limited") {
		t.Errorf("missing limited verdict: %q", out.String())
	}
}
