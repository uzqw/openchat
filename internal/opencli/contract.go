// Package opencli runs the locked @jackwener/opencli v1.8.7 contract as
// child processes: argv only (never a shell), explicit global --profile,
// --format json on business calls, a minimal child environment, and
// streaming size-limited output capture. Tests inject a fake executable
// through Execer.Path, so no real Gemini account or Chrome is ever touched.
package opencli

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Locked contract constants for @jackwener/opencli v1.8.7. Verified
// against the target host's `opencli --version` / `--help` output:
// --profile is a global flag before the subcommand, and the gemini
// subcommands share the -f/--format json option.
const (
	LockedVersion    = "1.8.7"
	ExitAuthRequired = 77
	// SentinelPrefix is the only recognized "no response" marker. It is
	// matched as a raw stdout prefix only (see IsSentinel), so identical
	// text inside Gemini Markdown is never misfired.
	SentinelPrefix = "💬 [NO RESPONSE]"
	FormatJSON     = "json"
)

// Outcome of one Gemini ask per the locked error contract.
type Outcome string

const (
	OutcomeSuccess      Outcome = "success"
	OutcomeAuthRequired Outcome = "auth_required"
	OutcomeFailed       Outcome = "failed"
	OutcomeUnknown      Outcome = "unknown_outcome"
)

// AskResult is the parsed JSON of a successful `gemini ask --format json`.
type AskResult struct {
	Response string `json:"response"`
}

// ParseAsk parses gemini ask JSON. The response field must be present: a
// structurally valid JSON object without it (e.g. an error envelope) is
// not a success. Real v1.8.7 wraps the answer in a top-level JSON array
// ([{"response": ...}]) while the documented legacy shape is a bare
// object; both are accepted. For the array form exactly one element with
// a present response field is a success.
func ParseAsk(stdout string) (AskResult, error) {
	var arr []struct {
		Response *string `json:"response"`
	}
	if err := json.Unmarshal([]byte(stdout), &arr); err == nil {
		if len(arr) == 1 && arr[0].Response != nil {
			return AskResult{Response: *arr[0].Response}, nil
		}
		return AskResult{}, errors.New("ask output missing response field")
	}
	var raw struct {
		Response *string `json:"response"`
	}
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return AskResult{}, err
	}
	if raw.Response == nil {
		return AskResult{}, errors.New("ask output missing response field")
	}
	return AskResult{Response: *raw.Response}, nil
}

// IsSentinel reports the locked "no Gemini response" marker as an exact
// stdout prefix, and nothing else.
func IsSentinel(stdout string) bool {
	return strings.HasPrefix(stdout, SentinelPrefix)
}

// appPathRe matches the /app/<id> path of a Gemini conversation URL.
var appPathRe = regexp.MustCompile(`^/app/([A-Za-z0-9_-]+)`)

// appIDRe matches a bare Gemini conversation id.
var appIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ParseConversationID extracts the Gemini conversation id from a full URL
// (https://gemini.google.com/app/<id>), a relative /app/<id> path, or a
// bare id. Returns "" when nothing usable is present.
func ParseConversationID(s string) string {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if m := appPathRe.FindStringSubmatch(u.Path); m != nil {
			return m[1]
		}
	}
	trimmed := strings.TrimPrefix(raw, "/app/")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		trimmed = trimmed[:i]
	}
	if appIDRe.MatchString(trimmed) {
		return trimmed
	}
	return ""
}

// ParseStatusURL extracts the current conversation URL from gemini status
// JSON. Real v1.8.7 status output is a top-level array with a "Url" field;
// the legacy bare-object shape is also accepted. Returns "" when absent.
func ParseStatusURL(stdout string) string {
	var arr []struct {
		URL string `json:"Url"`
	}
	if err := json.Unmarshal([]byte(stdout), &arr); err == nil {
		for _, m := range arr {
			if m.URL != "" {
				return m.URL
			}
		}
		return ""
	}
	var m struct {
		URL string `json:"Url"`
	}
	if err := json.Unmarshal([]byte(stdout), &m); err == nil {
		return m.URL
	}
	return ""
}

// StatusURLHasConversationID reports whether a gemini status output shows
// the persistent tab on the given conversation. It is the resume safety
// check: never ask in a context we did not deliberately navigate to.
func StatusURLHasConversationID(statusOut, wantID string) bool {
	return ParseConversationID(ParseStatusURL(statusOut)) == wantID
}

// AskOutcomeOf maps one ask execution Result to the locked outcome
// contract. failed is reserved for local spawn evidence (process never
// started); once the process started, a non-successful ask is always
// unknown_outcome, never a fake success.
func AskOutcomeOf(r Result) (Outcome, string) {
	if !r.Started {
		return OutcomeFailed, "spawn"
	}
	if r.ExitCode == ExitAuthRequired {
		return OutcomeAuthRequired, "exit 77"
	}
	if IsSentinel(r.Stdout) {
		return OutcomeUnknown, "sentinel"
	}
	if r.Overflow != "" {
		return OutcomeUnknown, "overflow:" + r.Overflow
	}
	if r.TimedOut {
		return OutcomeUnknown, "timeout"
	}
	if r.Canceled {
		return OutcomeUnknown, "canceled"
	}
	if r.ExitCode == 0 {
		parsed, err := ParseAsk(r.Stdout)
		if err != nil {
			return OutcomeUnknown, "bad_json"
		}
		// the sentinel sits inside the response field in real v1.8.7
		// output ([{"response":"💬 [NO RESPONSE]..."}]); the raw-prefix
		// check above only catches a bare (non-JSON) sentinel.
		if IsSentinel(parsed.Response) {
			return OutcomeUnknown, "sentinel"
		}
		return OutcomeSuccess, ""
	}
	return OutcomeUnknown, "exit " + strconv.Itoa(r.ExitCode)
}
