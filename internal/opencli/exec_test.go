package opencli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fakeOpenCLIPath string

// TestMain builds the fake opencli binary once; every exec-level test runs
// against it, so no real Gemini account or Chrome is ever touched.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fake-opencli-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake build dir:", err)
		os.Exit(1)
	}
	fakeOpenCLIPath = filepath.Join(dir, "opencli")
	build := exec.Command("go", "build", "-o", fakeOpenCLIPath, "./fakeopencli")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "build fake opencli:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func scenarioEnv(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func newFakeExecer(t *testing.T, scenario map[string]string) Execer {
	t.Helper()
	return Execer{
		Path:           fakeOpenCLIPath,
		ExtraEnv:       scenarioEnv(scenario),
		Timeout:        10 * time.Second,
		MaxStdoutBytes: 64 << 10,
		MaxStderrBytes: 16 << 10,
	}
}

func TestRunAskSuccessRoundtrip(t *testing.T) {
	e := newFakeExecer(t, map[string]string{
		"FAKE_OPENCLI_DELAY_MS": "150", // "wait" scenario: still a success
		"FAKE_OPENCLI_STDOUT":   `{"response":"你好，Gemini"}`,
	})
	r := e.Run(context.Background(), SiteGemini.AskArgs("p1", AskOpts{Prompt: "你好"})...)
	if !r.Started || r.StartErr != nil {
		t.Fatalf("spawn: started=%v err=%v", r.Started, r.StartErr)
	}
	if r.ExitCode != 0 {
		t.Fatalf("exit = %d, want 0 (stdout %q)", r.ExitCode, r.Stdout)
	}
	if o, _ := SiteGemini.AskOutcomeOf(r); o != OutcomeSuccess {
		t.Fatalf("outcome = %s, want success", o)
	}
	res, err := ParseAsk(r.Stdout)
	if err != nil || res.Response != "你好，Gemini" {
		t.Fatalf("parse %q: res=%+v err=%v", r.Stdout, res, err)
	}
}

func TestRunVersionProbe(t *testing.T) {
	e := newFakeExecer(t, nil)
	r := e.Run(context.Background(), VersionArgs()...)
	if got := strings.TrimSpace(r.Stdout); got != LockedVersion {
		t.Fatalf("version = %q, want %q", got, LockedVersion)
	}
}

func TestRunVersionMismatchVisible(t *testing.T) {
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_VERSION": "9.9.9"})
	r := e.Run(context.Background(), VersionArgs()...)
	if got := strings.TrimSpace(r.Stdout); got != "9.9.9" {
		t.Fatalf("version = %q, want fake override", got)
	}
}

func TestRunStartFailureIsFailed(t *testing.T) {
	e := Execer{Path: "/no/such/opencli", Timeout: 5 * time.Second, MaxStdoutBytes: 4096, MaxStderrBytes: 4096}
	r := e.Run(context.Background(), SiteGemini.AskArgs("p1", AskOpts{Prompt: "x"})...)
	if r.Started || r.StartErr == nil {
		t.Fatalf("started=%v want started=false with StartErr", r.Started)
	}
	if o, reason := SiteGemini.AskOutcomeOf(r); o != OutcomeFailed || reason != "spawn" {
		t.Fatalf("outcome = %s (%s), want failed (spawn)", o, reason)
	}
}

func TestRunTimeoutKillsAndMapsUnknown(t *testing.T) {
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_DELAY_MS": "30000"})
	e.Timeout = 300 * time.Millisecond
	start := time.Now()
	r := e.Run(context.Background(), SiteGemini.AskArgs("p1", AskOpts{Prompt: "x"})...)
	if !r.TimedOut || r.Canceled {
		t.Fatalf("timedOut=%v canceled=%v, want timedOut", r.TimedOut, r.Canceled)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("not killed promptly: %v", d)
	}
	if o, reason := SiteGemini.AskOutcomeOf(r); o != OutcomeUnknown || reason != "timeout" {
		t.Fatalf("outcome = %s (%s), want unknown_outcome (timeout)", o, reason)
	}
}

func TestRunCancelKillsAndMapsUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_DELAY_MS": "30000"})
	e.Timeout = 0
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	r := e.Run(ctx, SiteGemini.AskArgs("p1", AskOpts{Prompt: "x"})...)
	if !r.Canceled || r.TimedOut {
		t.Fatalf("canceled=%v timedOut=%v, want canceled", r.Canceled, r.TimedOut)
	}
	if o, reason := SiteGemini.AskOutcomeOf(r); o != OutcomeUnknown || reason != "canceled" {
		t.Fatalf("outcome = %s (%s), want unknown_outcome (canceled)", o, reason)
	}
}

func TestRunStdoutOverflowKillsAndMapsUnknown(t *testing.T) {
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_STDOUT_BYTES": "4096"})
	e.MaxStdoutBytes = 1024
	r := e.Run(context.Background(), SiteGemini.AskArgs("p1", AskOpts{Prompt: "x"})...)
	if r.Overflow != "stdout" {
		t.Fatalf("overflow = %q, want stdout", r.Overflow)
	}
	if len(r.Stdout) != 1024 {
		t.Fatalf("captured %d bytes, want capped at 1024", len(r.Stdout))
	}
	if o, reason := SiteGemini.AskOutcomeOf(r); o != OutcomeUnknown || !strings.Contains(reason, "overflow") {
		t.Fatalf("outcome = %s (%s), want unknown_outcome (overflow)", o, reason)
	}
}

func TestRunStderrOverflowCapped(t *testing.T) {
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_STDERR_BYTES": "4096"})
	e.MaxStderrBytes = 1024
	r := e.Run(context.Background(), SiteGemini.AskArgs("p1", AskOpts{Prompt: "x"})...)
	if r.Overflow != "stderr" {
		t.Fatalf("overflow = %q, want stderr", r.Overflow)
	}
	if len(r.Stderr) != 1024 {
		t.Fatalf("captured %d stderr bytes, want capped at 1024", len(r.Stderr))
	}
}

func TestRunChildEnvIsMinimal(t *testing.T) {
	os.Setenv("NODE_OPTIONS", "--require /tmp/evil.js")
	os.Setenv("NODE_PATH", "/tmp/evil")
	defer func() {
		os.Unsetenv("NODE_OPTIONS")
		os.Unsetenv("NODE_PATH")
	}()
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_ECHO_ENV": "1"})
	r := e.Run(context.Background(), SiteGemini.AskArgs("p1", AskOpts{Prompt: "x"})...)
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if len(lines) == 0 {
		t.Fatalf("no env echoed: %q", r.Stdout)
	}
	for _, ln := range lines {
		k, _, _ := strings.Cut(ln, "=")
		switch k {
		case "PATH", "HOME", "TMPDIR": // allowlist only
		default:
			t.Fatalf("leaked env var %q into child (line %q)", k, ln)
		}
	}
}

func TestRunArgsNeverShellSplits(t *testing.T) {
	prompt := `hi; echo injected & rm -rf / "$(touch /tmp/pwned)"`
	e := newFakeExecer(t, map[string]string{"FAKE_OPENCLI_ECHO_ARGS": "1"})
	want := SiteGemini.AskArgs("p1", AskOpts{Prompt: prompt})
	r := e.Run(context.Background(), want...)
	got := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv changed or shell-split:\n got %q\nwant %q", got, want)
	}
	if want[4] != prompt {
		t.Fatalf("prompt not passed verbatim: %q", want[4])
	}
}
