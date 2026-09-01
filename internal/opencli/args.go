package opencli

import (
	"strconv"
	"time"
)

// Thinking levels for site ask. Empty means "leave the site's current
// value unchanged". v1.8.7 cannot discover per-model thinking capability,
// so only the enum is validated here; real selection happens in the site
// UI (gemini ask has no --thinking beyond the locked enum).
const (
	ThinkingStandard = "standard"
	ThinkingExtended = "extended"
)

// AskOpts are the locked site ask options.
type AskOpts struct {
	Prompt   string
	New      bool
	Model    string // canonical id (e.g. "2.5-flash"); empty = site default
	Thinking string // "", "standard" or "extended"
	// Timeout is the adapter-side --timeout in seconds (0 = opencli default).
	Timeout time.Duration
}

// Validate checks the request-level bounds locked by the contract for the
// given site: the thinking enum, and fail-closed capability checks (a
// site without --model/--thinking must never receive either).
func (o AskOpts) Validate(s *Site) error {
	if s == nil {
		s = SiteGemini
	}
	if o.Thinking != "" {
		if !s.Thinking {
			return &ErrUnsupportedOption{Site: s.Name, Option: "thinking"}
		}
		switch o.Thinking {
		case ThinkingStandard, ThinkingExtended:
		default:
			return &ErrInvalidThinking{o.Thinking}
		}
	}
	if o.Model != "" && !s.ModelPick {
		return &ErrUnsupportedOption{Site: s.Name, Option: "model"}
	}
	return nil
}

// ErrInvalidThinking reports a thinking value outside the locked enum.
type ErrInvalidThinking struct{ Value string }

func (e *ErrInvalidThinking) Error() string {
	return "thinking must be empty, " + ThinkingStandard + ", or " + ThinkingExtended + " (got " + strconv.Quote(e.Value) + ")"
}

// ErrUnsupportedOption reports a capability the site adapter does not
// accept (model/thinking beyond the site's capability). Fail closed: never round-trip
// to a real ask and die mid-flight with a usage error.
type ErrUnsupportedOption struct {
	Site   string
	Option string
}

func (e *ErrUnsupportedOption) Error() string {
	return e.Site + " ask does not support --" + e.Option
}

// VersionArgs returns argv for: opencli --version.
func VersionArgs() []string { return []string{"--version"} }

// DoctorArgs returns argv for: opencli --profile <p> doctor.
func DoctorArgs(profile string) []string {
	return []string{"--profile", profile, "doctor"}
}
