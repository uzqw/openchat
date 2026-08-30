package opencli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// envAllowlist is the only environment an opencli child receives:
// NODE_OPTIONS, NODE_PATH and every other variable can never leak in,
// and the child can never be influenced by the parent's Node settings.
var envAllowlist = map[string]bool{"PATH": true, "HOME": true, "TMPDIR": true}

// ChildEnv builds the minimal child environment from a parent environment.
func ChildEnv(parent []string) []string {
	byKey := make(map[string]string, len(parent))
	for _, kv := range parent {
		if k, v, ok := strings.Cut(kv, "="); ok {
			byKey[k] = v
		}
	}
	out := make([]string, 0, len(envAllowlist))
	for _, k := range []string{"PATH", "HOME", "TMPDIR"} {
		if v, ok := byKey[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// Default capture limits when the config leaves them zero.
const (
	DefaultMaxStdoutBytes = 4 << 20
	DefaultMaxStderrBytes = 1 << 20
)

// Execer executes opencli commands through exec.CommandContext — never a
// shell. Path is the opencli executable (default "opencli"); tests point
// it at a fake binary so no real Gemini or Chrome is ever touched.
type Execer struct {
	Path           string
	ExtraEnv       []string // appended after ChildEnv (test scenario config)
	Timeout        time.Duration
	MaxStdoutBytes int
	MaxStderrBytes int
}

// Result of one child run.
type Result struct {
	Started  bool
	StartErr error
	ExitCode int
	Stdout   string
	Stderr   string
	TimedOut bool
	Canceled bool
	Overflow string // "stdout", "stderr" or "stdout,stderr" when a capture limit was hit
}

// Run executes args as one argv vector. stdout/stderr are streamed with a
// size limit; crossing a limit kills the child immediately (still drained
// so Wait completes) and is reported via Overflow.
func (e *Execer) Run(ctx context.Context, args ...string) Result {
	maxOut, maxErr := e.MaxStdoutBytes, e.MaxStderrBytes
	if maxOut <= 0 {
		maxOut = DefaultMaxStdoutBytes
	}
	if maxErr <= 0 {
		maxErr = DefaultMaxStderrBytes
	}

	execCtx, cancel := context.WithCancel(ctx)
	if e.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(execCtx, e.Timeout)
	}
	defer cancel()

	path := e.Path
	if path == "" {
		path = "opencli"
	}
	cmd := exec.CommandContext(execCtx, path, args...)
	cmd.Env = append(ChildEnv(os.Environ()), e.ExtraEnv...)
	cmd.WaitDelay = 2 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{StartErr: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{StartErr: err}
	}
	if err := cmd.Start(); err != nil {
		return Result{StartErr: err} // spawn failure -> locked "failed"
	}

	var outR, errR struct {
		text     string
		exceeded bool
		done     chan struct{}
	}
	outR.done, errR.done = make(chan struct{}), make(chan struct{})
	go func() {
		outR.text, outR.exceeded = readLimited(stdout, maxOut, cancel)
		close(outR.done)
	}()
	go func() {
		errR.text, errR.exceeded = readLimited(stderr, maxErr, cancel)
		close(errR.done)
	}()

	_ = cmd.Wait()
	// Unblock readers if a descendant kept a pipe open past process exit.
	stdout.Close()
	stderr.Close()
	<-outR.done
	<-errR.done

	r := Result{
		Started:  true,
		ExitCode: cmd.ProcessState.ExitCode(),
		Stdout:   outR.text,
		Stderr:   errR.text,
	}
	switch {
	case outR.exceeded && errR.exceeded:
		r.Overflow = "stdout,stderr"
	case outR.exceeded:
		r.Overflow = "stdout"
	case errR.exceeded:
		r.Overflow = "stderr"
	}
	switch execCtx.Err() {
	case context.DeadlineExceeded:
		r.TimedOut = true
	case context.Canceled:
		r.Canceled = true
	}
	return r
}

// readLimited streams r, storing at most limit bytes, and calls kill the
// moment the limit is crossed; the rest of the stream is drained to EOF so
// Wait can always complete.
func readLimited(r io.Reader, limit int, kill func()) (string, bool) {
	var b strings.Builder
	buf := make([]byte, 32<<10)
	n, exceeded := 0, false
	for {
		m, err := r.Read(buf)
		if m > 0 {
			n += m
			if !exceeded {
				if n <= limit {
					b.Write(buf[:m])
				} else {
					if room := limit - (n - m); room > 0 {
						b.Write(buf[:room])
					}
					exceeded = true
					kill()
				}
			}
		}
		if err != nil {
			break
		}
	}
	return b.String(), exceeded
}
