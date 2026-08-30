// Command fake-opencli is a test double for @jackwener/opencli v1.8.7.
// It is only ever invoked by tests through internal/opencli.Execer.Path;
// the scenario is configured solely with FAKE_OPENCLI_* env vars so the
// argv under test stays byte-for-byte what the real CLI would receive.
// It enforces the locked argv contract: every non-version call carries a
// global --profile, and every gemini subcommand carries --format json.
package main

import (
	"fmt"
	"os"
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
