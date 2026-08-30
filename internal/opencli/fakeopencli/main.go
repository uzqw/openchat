// Command fake-opencli is a test double for @jackwener/opencli v1.8.7.
// It is only ever invoked by tests through internal/opencli.Execer.Path;
// the scenario is configured with FAKE_OPENCLI_* env vars or by [FAKE:...]
// markers embedded in the ask prompt, so the argv under test stays
// byte-for-byte what the real CLI would receive. It enforces the locked
// argv contract: every non-version call carries a global --profile, and
// every gemini subcommand carries --format json.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"openchat/internal/opencli"
)

func envOr(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, ok := os.LookupEnv(k); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func failExit(code int, msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(code)
}

func main() {
	args := os.Args[1:]

	// Version contract: opencli --version prints the locked version.
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(envOr("FAKE_OPENCLI_VERSION", opencli.LockedVersion))
		return
	}

	if !contains(args, "--profile") {
		failExit(2, "fake opencli: missing global --profile")
	}
	if i := indexOf(args, "gemini"); i >= 0 && !hasFormatJSON(args[i+1:]) {
		failExit(2, "fake opencli: gemini command missing --format json")
	}

	// Prompt markers make the fake deterministic per task without touching
	// the runner or the environment. The real CLI is never affected: real
	// prompts do not contain [FAKE:...] text.
	if sc := promptScenario(askPrompt(args)); sc != nil {
		applyScenario(sc, args)
		return
	}

	if d := envInt("FAKE_OPENCLI_DELAY_MS", 0); d > 0 {
		time.Sleep(time.Duration(d) * time.Millisecond)
	}

	switch {
	case envInt("FAKE_OPENCLI_ECHO_ARGS", 0) == 1:
		fmt.Println(strings.Join(args, "\n"))
	case envInt("FAKE_OPENCLI_ECHO_ENV", 0) == 1:
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if k == "PATH" || k == "HOME" || k == "TMPDIR" || k == "NODE_OPTIONS" || k == "NODE_PATH" {
				fmt.Println(kv)
			}
		}
	default:
		writeStream(os.Stdout, "FAKE_OPENCLI_STDOUT", "FAKE_OPENCLI_STDOUT_BYTES")
		writeStream(os.Stderr, "FAKE_OPENCLI_STDERR", "FAKE_OPENCLI_STDERR_BYTES")
	}
	os.Exit(envInt("FAKE_OPENCLI_EXIT", 0))
}

func writeStream(f *os.File, textEnv, bytesEnv string) {
	if n := envInt(bytesEnv, 0); n > 0 {
		fmt.Fprint(f, strings.Repeat("x", n))
		return
	}
	if s := envOr(textEnv, ""); s != "" {
		fmt.Fprint(f, s)
	}
}

func contains(args []string, s string) bool {
	return indexOf(args, s) >= 0
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func hasFormatJSON(args []string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--format" && args[i+1] == "json" {
			return true
		}
	}
	return false
}

// ---- prompt-marker scenarios ---------------------------------------------

// markerRe matches [FAKE:kind] and [FAKE:kind:value] inside a prompt.
var markerRe = regexp.MustCompile(`\[FAKE:([a-z0-9-]+)(?::([^\]]*))?\]`)

// scenario is the fake behavior requested by the prompt markers.
type scenario struct {
	timeout  bool // sleep forever (the runner's kill ceiling decides)
	delayMS  int
	exit     *int
	sentinel bool   // emit the locked 💬 [NO RESPONSE] prefix
	stdout   string // raw stdout to emit (may be empty)
	stderr   string
	bytes    int  // emit N bytes of 'x' to stdout
	echo     bool // echo argv as the {"response": ...} JSON

	// exit-once: on the first run (marker file absent) exit with this code
	// and create the file; later runs ignore it. Value is "CODE:PATH".
	exitOnceCode int
	exitOnceFile string
}

// askPrompt returns the prompt argument (the argv element right after
// "ask") or "" when this is not an ask command.
func askPrompt(args []string) string {
	for i, a := range args {
		if a == "ask" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func promptScenario(prompt string) *scenario {
	if prompt == "" {
		return nil
	}
	sc := &scenario{}
	found := false
	for _, m := range markerRe.FindAllStringSubmatch(prompt, -1) {
		kind, val := m[1], m[2]
		switch kind {
		case "timeout":
			sc.timeout, found = true, true
		case "delay":
			sc.delayMS, found = envIntOf(val, 0), true
		case "exit":
			n := envIntOf(val, 0)
			sc.exit = &n
			found = true
		case "exit-once":
			if codeStr, path, ok := strings.Cut(val, ":"); ok {
				sc.exitOnceCode = envIntOf(codeStr, 1)
				sc.exitOnceFile = path
				found = true
			}
		case "sentinel":
			sc.sentinel, found = true, true
		case "stdout":
			sc.stdout, found = val, true
		case "stderr":
			sc.stderr, found = val, true
		case "bytes":
			sc.bytes, found = envIntOf(val, 0), true
		case "echo-args":
			sc.echo, found = true, true
		}
	}
	if !found {
		return nil
	}
	return sc
}

func envIntOf(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func applyScenario(sc *scenario, args []string) {
	if sc.exitOnceFile != "" {
		if _, err := os.Stat(sc.exitOnceFile); err != nil {
			_ = os.WriteFile(sc.exitOnceFile, []byte("ran"), 0o600)
			os.Exit(sc.exitOnceCode)
		}
	}
	if sc.delayMS > 0 {
		time.Sleep(time.Duration(sc.delayMS) * time.Millisecond)
	}
	if sc.timeout {
		time.Sleep(30 * time.Second) // killed by the runner's kill ceiling
	}
	if sc.stdout != "" {
		fmt.Fprint(os.Stdout, sc.stdout)
	} else if sc.sentinel {
		fmt.Fprint(os.Stdout, opencli.SentinelPrefix+" No Gemini response within 60s.")
	} else if sc.echo {
		payload, err := json.Marshal(map[string]string{"response": strings.Join(args, "\n")})
		if err != nil {
			failExit(3, "fake opencli: echo marshal failed")
		}
		fmt.Fprintln(os.Stdout, string(payload))
	}
	if sc.bytes > 0 {
		fmt.Fprint(os.Stdout, strings.Repeat("x", sc.bytes))
	}
	if sc.stderr != "" {
		fmt.Fprint(os.Stderr, sc.stderr)
	}
	if sc.exit != nil {
		os.Exit(*sc.exit)
	}
	os.Exit(0)
}
