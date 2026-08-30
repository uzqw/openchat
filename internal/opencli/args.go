package opencli

import (
	"strconv"
	"time"
)

// Thinking levels for gemini ask. Empty means "leave the site's current
// value unchanged". v1.8.7 cannot discover per-model thinking capability,
// so only the enum is validated here; real selection happens in the
// Gemini UI.
const (
	ThinkingStandard = "standard"
	ThinkingExtended = "extended"
)

// AskOpts are the locked gemini ask options.
type AskOpts struct {
	Prompt   string
	New      bool
	Model    string // canonical id (e.g. "2.5-flash"); empty = site default
	Thinking string // "", "standard" or "extended"
	// Timeout is the adapter-side --timeout in seconds (0 = opencli default).
	Timeout time.Duration
}

// Validate checks the request-level bounds locked by the contract.
func (o AskOpts) Validate() error {
	switch o.Thinking {
	case "", ThinkingStandard, ThinkingExtended:
		return nil
	}
	return &ErrInvalidThinking{o.Thinking}
}

// ErrInvalidThinking reports a thinking value outside the locked enum.
type ErrInvalidThinking struct{ Value string }

func (e *ErrInvalidThinking) Error() string {
	return "thinking must be empty, " + ThinkingStandard + ", or " + ThinkingExtended + " (got " + strconv.Quote(e.Value) + ")"
}

// VersionArgs returns argv for: opencli --version.
func VersionArgs() []string { return []string{"--version"} }

// DoctorArgs returns argv for: opencli --profile <p> doctor.
func DoctorArgs(profile string) []string {
	return []string{"--profile", profile, "doctor"}
}

// AskArgs returns argv for a gemini ask with the locked flags, always
// requesting JSON output. Argument order is pinned and asserted by tests.
func AskArgs(profile string, o AskOpts) []string {
	a := []string{"--profile", profile, "gemini", "ask", o.Prompt}
	if o.New {
		a = append(a, "--new", "true")
	}
	if o.Model != "" {
		a = append(a, "--model", o.Model)
	}
	if o.Thinking != "" {
		a = append(a, "--thinking", o.Thinking)
	}
	if o.Timeout > 0 {
		a = append(a, "--timeout", strconv.Itoa(int(o.Timeout.Seconds())))
	}
	return append(a, "--format", FormatJSON)
}

// ModelsArgs returns argv for: opencli --profile <p> gemini models --format json.
func ModelsArgs(profile string) []string {
	return []string{"--profile", profile, "gemini", "models", "--format", FormatJSON}
}

// StatusArgs returns argv for: opencli --profile <p> gemini status --format json.
func StatusArgs(profile string) []string {
	return []string{"--profile", profile, "gemini", "status", "--format", FormatJSON}
}

// DetailArgs returns argv for: opencli --profile <p> gemini detail <id> --format json.
// detail navigates the persistent site session tab to the given Gemini
// conversation (bare id, /app/<id> path or full URL) and reads its turns;
// it is the resume primitive for archived conversations.
func DetailArgs(profile, id string) []string {
	return []string{"--profile", profile, "gemini", "detail", id, "--format", FormatJSON}
}

// WhoamiArgs returns argv for: opencli --profile <p> gemini whoami --format json.
func WhoamiArgs(profile string) []string {
	return []string{"--profile", profile, "gemini", "whoami", "--format", FormatJSON}
}

// LoginArgs returns argv for: opencli --profile <p> gemini login --format json.
func LoginArgs(profile string) []string {
	return []string{"--profile", profile, "gemini", "login", "--format", FormatJSON}
}
