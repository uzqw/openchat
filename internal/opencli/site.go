// Site describes one OpenCLI site adapter ("gemini", "grok"): its
// subcommand name, display label and the capabilities that differ between
// sites. Everything site-specific in the platform routes through this one
// table, so a new provider is a new Site entry plus contract verification
// (docs/opencli-contract.md) — no per-site branches spread across layers.
package opencli

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Site is the immutable capability table for one OpenCLI site adapter.
type Site struct {
	// Name is the opencli subcommand ("gemini", "grok").
	Name string
	// Label is the human display name ("Gemini", "Grok").
	Label string
	// ModelPick: the ask command accepts --model.
	ModelPick bool
	// Thinking: the ask command accepts --thinking.
	Thinking bool
	// ModelsCmd: the site has a models subcommand to discover models.
	ModelsCmd bool
	// Sentinel: ask emits the 💬 [NO RESPONSE] marker inside a successful
	// (exit 0) response when no answer appears (gemini only; grok throws
	// timeout exit 75 instead).
	Sentinel bool

	// Conversation URL shape: <pathPrefix>/<id> and the bare-id pattern.
	convPathRe *regexp.Regexp
	convPrefix string
	convIDRe   *regexp.Regexp
}

// SiteGemini is the default provider (locked contract, v1.8.7).
var SiteGemini = &Site{
	Name:       "gemini",
	Label:      "Gemini",
	ModelPick:  true, // gemini ask --model <canonical-id>
	Thinking:   true, // gemini ask --thinking standard|extended
	ModelsCmd:  true, // gemini models
	Sentinel:   true, // 💬 [NO RESPONSE] marker
	convPathRe: regexp.MustCompile(`^/app/([A-Za-z0-9_-]+)`),
	convPrefix: "/app/",
	convIDRe:   regexp.MustCompile(`^[A-Za-z0-9_-]+$`),
}

// UUIDv4 8-4-4-4-12, the format grok.com uses for /c/<id>.
var grokIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SiteGrok is the grok.com adapter (same opencli v1.8.7 lock). It has no
// model discovery, no --model/--thinking knobs, and no sentinel marker —
// grok ask reports a missing answer as timeout exit 75.
var SiteGrok = &Site{
	Name:       "grok",
	Label:      "Grok",
	ModelPick:  false,
	Thinking:   false,
	ModelsCmd:  false,
	Sentinel:   false,
	convPathRe: regexp.MustCompile(`^/c/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`),
	convPrefix: "/c/",
	convIDRe:   grokIDRe,
}

// Sites is the registry of supported provider sites, ordered by Name.
var Sites = []*Site{SiteGemini, SiteGrok}

// ByName resolves a site by its opencli subcommand name ("gemini" /
// "grok"), or returns an error for anything else (fail closed at startup).
func ByName(name string) (*Site, error) {
	if name == "" {
		return SiteGemini, nil
	}
	for _, s := range Sites {
		if s.Name == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unsupported OPENCLI_SITE %q (supported: %s)", name, SiteNames())
}

// SiteNames returns "gemini, grok" style list for error messages.
func SiteNames() string {
	names := make([]string, len(Sites))
	for i, s := range Sites {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// AskArgs returns argv for: opencli --profile <p> <site> ask ... with the
// locked flags (only the ones the site actually accepts). Argument order
// is pinned and asserted by tests.
func (s *Site) AskArgs(profile string, o AskOpts) []string {
	a := []string{"--profile", profile, s.Name, "ask", o.Prompt}
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

// ModelsArgs returns argv for: opencli --profile <p> <site> models --format json.
func (s *Site) ModelsArgs(profile string) []string {
	return []string{"--profile", profile, s.Name, "models", "--format", FormatJSON}
}

// StatusArgs returns argv for: opencli --profile <p> <site> status --format json.
func (s *Site) StatusArgs(profile string) []string {
	return []string{"--profile", profile, s.Name, "status", "--format", FormatJSON}
}

// DetailArgs returns argv for: opencli --profile <p> <site> detail <id> --format json.
// detail navigates the persistent site session tab to the given
// conversation (bare id or full URL) and reads its turns; it is the
// resume primitive for archived conversations.
func (s *Site) DetailArgs(profile, id string) []string {
	return []string{"--profile", profile, s.Name, "detail", id, "--format", FormatJSON}
}

// WhoamiArgs returns argv for: opencli --profile <p> <site> whoami --format json.
func (s *Site) WhoamiArgs(profile string) []string {
	return []string{"--profile", profile, s.Name, "whoami", "--format", FormatJSON}
}

// LoginArgs returns argv for: opencli --profile <p> <site> login --format json.
func (s *Site) LoginArgs(profile string) []string {
	return []string{"--profile", profile, s.Name, "login", "--format", FormatJSON}
}

// ConversationID extracts the site conversation id from a full URL
// (https://.../c/<id> or .../app/<id>), a relative path, or a bare id.
// Returns "" when nothing usable is present.
func (s *Site) ConversationID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if m := s.convPathRe.FindStringSubmatch(u.Path); m != nil {
			return s.normalizeID(m[1])
		}
	}
	trimmed := strings.TrimPrefix(raw, s.convPrefix)
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		trimmed = trimmed[:i]
	}
	if s.convIDRe.MatchString(trimmed) {
		return s.normalizeID(trimmed)
	}
	return ""
}

// normalizeID lowercases grok ids (grok.com emits lowercase UUIDs; the
// opencli adapter also lowercases). Gemini ids are already lowercase hex.
func (s *Site) normalizeID(id string) string {
	if s == SiteGrok {
		return strings.ToLower(id)
	}
	return id
}

// StatusURLHasConversationID reports whether a status output shows the
// persistent tab on the given conversation. It is the resume safety
// check: never ask in a context we did not deliberately navigate to.
func (s *Site) StatusURLHasConversationID(statusOut, wantID string) bool {
	return s.ConversationID(ParseStatusURL(statusOut)) == wantID
}
