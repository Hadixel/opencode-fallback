// Command ocf runs OpenCode against a chain of models and moves on when a
// provider says its free tier is exhausted.
//
// The problem it solves: OpenCode's free tier is enforced server-side. The TLS
// client fingerprint decides whether free models are usable at all, and the free
// allowance is counted per source IP. When the answer is "Free usage limit
// reached", no local switch resets it. ocf's answer is to treat a limit as a
// routing signal: try the next model in the chain, and finish the work on a
// provider the caller is entitled to.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const version = "0.1.0"

// defaultChain is what a stock OpenCode install can use with no paid key: the
// free models of the built-in opencode provider.
var defaultChain = []string{
	"opencode/deepseek-v4.1-flash-free",
	"opencode/glm-5.3-flash-free",
	"opencode/big-pickle",
}

// stdout and stderr are variables so tests can capture the wrapper's own output.
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// egressIPFn is the seam for the status check (a unit test must not reach the
// network).
var egressIPFn = egressIP

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage(stderr)
		os.Exit(2)
	}
	var code int
	switch args[0] {
	case "run":
		code = runCmd(args[1:])
	case "check":
		code = checkCmd(args[1:])
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, "ocf", version)
	case "help", "-h", "--help":
		usage(stdout)
	default:
		fmt.Fprintf(stderr, "ocf: unknown command %q\n\n", args[0])
		usage(stderr)
		code = 2
	}
	os.Exit(code)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `ocf — run OpenCode with a model chain

Usage:
  ocf run [flags] <prompt...>
  ocf check [flags]
  ocf version

run:
  Tries each provider/model in the chain until one answers. A free-tier limit
  ("Free usage limit reached") or an unavailable model moves to the next entry.

  flags:
    --chain LIST    comma-separated provider/model list
    --cwd DIR       run OpenCode in DIR
    --timeout DUR   per-model timeout (default 10m)
    --prompt TEXT   prompt (otherwise the positional arguments)
    --quiet         hide the fallback banners

check:
  Prints the current egress IP and whether each model in the chain answers.

Chain defaults to $OCF_CHAIN, then:
  `+strings.Join(defaultChain, "\n  ")+`

Exit codes: 0 answered, 1 hard failure, 2 usage, 3 all limited/unavailable.
`)
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	chainFlag := fs.String("chain", envOr("OCF_CHAIN", strings.Join(defaultChain, ",")), "comma-separated provider/model chain")
	cwd := fs.String("cwd", "", "run OpenCode in this directory")
	timeout := fs.Duration("timeout", 10*time.Minute, "per-model timeout")
	promptFlag := fs.String("prompt", "", "prompt (or pass it as arguments)")
	quiet := fs.Bool("quiet", false, "hide fallback banners")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	prompt := strings.TrimSpace(*promptFlag)
	if prompt == "" {
		prompt = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}
	if prompt == "" {
		fmt.Fprintln(stderr, "ocf run: a prompt is required")
		return 2
	}
	models := splitChain(*chainFlag)
	if len(models) == 0 {
		fmt.Fprintln(stderr, "ocf run: the chain is empty")
		return 2
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		fmt.Fprintln(stderr, "ocf run: opencode is not on PATH")
		return 1
	}

	for _, model := range models {
		start := time.Now()
		outcome, text := attempt(model, prompt, *cwd, *timeout, true)
		switch outcome {
		case resultOK:
			return 0
		case resultLimited:
			if !*quiet {
				banner("%s is rate-limited (%s) — trying the next model", model, short(text))
			}
		case resultUnavailable:
			if !*quiet {
				banner("%s is unavailable (%s) — skipping", model, short(text))
			}
		default:
			fmt.Fprint(stderr, text)
			if text != "" && !strings.HasSuffix(text, "\n") {
				fmt.Fprintln(stderr)
			}
			fmt.Fprintf(stderr, "ocf: %s failed after %s\n", model, time.Since(start).Round(time.Millisecond))
			return 1
		}
	}

	fmt.Fprintf(stderr, "ocf: every model in the chain is limited or unavailable:\n  %s\n", strings.Join(models, "\n  "))
	fmt.Fprintln(stderr, "ocf: the free tier is server-side and counted per source IP; add a paid model to --chain, wait for the window, or switch provider.")
	return 3
}

func checkCmd(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	chainFlag := fs.String("chain", envOr("OCF_CHAIN", strings.Join(defaultChain, ",")), "comma-separated provider/model chain")
	timeout := fs.Duration("timeout", 2*time.Minute, "per-model timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	models := splitChain(*chainFlag)
	if len(models) == 0 {
		fmt.Fprintln(stderr, "ocf check: the chain is empty")
		return 2
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		fmt.Fprintln(stderr, "ocf check: opencode is not on PATH")
		return 1
	}

	ip, err := egressIPFn(8 * time.Second)
	if err != nil {
		fmt.Fprintf(stdout, "egress: unknown (%v)\n", err)
	} else {
		fmt.Fprintf(stdout, "egress: %s\n", ip)
	}

	code := 0
	for _, model := range models {
		start := time.Now()
		outcome, text := attempt(model, "Reply with exactly: ok", "", *timeout, false)
		took := time.Since(start).Round(time.Millisecond)
		switch outcome {
		case resultOK:
			fmt.Fprintf(stdout, "  %-46s ok        %s\n", model, took)
		case resultLimited:
			fmt.Fprintf(stdout, "  %-46s limited   %s\n", model, took)
			code = 3
		case resultUnavailable:
			fmt.Fprintf(stdout, "  %-46s unavailable\n", model)
			if code == 0 {
				code = 1
			}
		default:
			fmt.Fprintf(stdout, "  %-46s failed    %s\n", model, short(text))
			if code == 0 {
				code = 1
			}
		}
	}
	return code
}

type outcome int

const (
	resultOK outcome = iota
	resultLimited
	resultUnavailable
	resultFailed
)

// attempt runs one model through the official client. stream mirrors its output
// while the result is being classified; check passes false so a status probe
// does not print model chatter.
func attempt(model, prompt, cwd string, timeout time.Duration, stream bool) (outcome, string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := opencodeCmd(ctx, "run", "-m", model, prompt)
	if err != nil {
		return resultFailed, fmt.Sprintf("ocf: %v\n", err)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	var buf bytes.Buffer
	if stream {
		cmd.Stdout = io.MultiWriter(stdout, &buf)
		cmd.Stderr = io.MultiWriter(stderr, &buf)
	} else {
		cmd.Stdout = &buf
		cmd.Stderr = &buf
	}
	runErr := cmd.Run()
	text := buf.String()

	switch {
	case isLimited(text):
		return resultLimited, text
	case isUnavailable(text):
		return resultUnavailable, text
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return resultFailed, fmt.Sprintf("ocf: %s timed out after %s\n", model, timeout)
	case runErr != nil:
		// opencode prints its own diagnostics; keep them, then name the exit.
		return resultFailed, text + fmt.Sprintf("ocf: %s exited: %v\n", model, runErr)
	default:
		return resultOK, text
	}
}

// opencodeCmd builds the command that launches the official client. On Windows
// the npm shim is a .cmd file, which CreateProcess cannot start directly, so it
// is run through cmd.exe instead of relying on path-resolution quirks.
func opencodeCmd(ctx context.Context, args ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".cmd", ".bat":
			return exec.CommandContext(ctx, "cmd.exe", append([]string{"/c", path}, args...)...), nil
		}
	}
	return exec.CommandContext(ctx, path, args...), nil
}

var limitedMarkers = []string{
	"free usage limit reached",
	"free usage limit",
	"freetiererror",
	"free tier can only be used from within opencode",
	"rate limit reached",
	"too many requests",
	"http 429",
	"429 too many",
}

var unavailableMarkers = []string{
	"no active credentials for provider",
	"model_not_found",
	"model not found",
	"unknown model",
	"provider not found",
	"no such provider",
	"invalid model",
}

func isLimited(s string) bool     { return containsAny(s, limitedMarkers) }
func isUnavailable(s string) bool { return containsAny(s, unavailableMarkers) }
func containsAny(s string, marks []string) bool {
	l := strings.ToLower(s)
	for _, m := range marks {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

func banner(format string, a ...any) {
	fmt.Fprintf(stderr, "ocf: "+format+"\n", a...)
}

// short picks the line that carries the reason, for a one-line banner.
func short(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no output"
	}
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		if strings.Contains(low, "limit") || strings.Contains(low, "error") ||
			strings.Contains(low, "credential") || strings.Contains(low, "429") {
			return truncate(l)
		}
	}
	return truncate(strings.Split(s, "\n")[0])
}

func truncate(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitChain(s string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func egressIP(timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ipify: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(b))
	if ip == "" {
		return "", errors.New("ipify: empty answer")
	}
	return ip, nil
}
