package opencli

import (
	"reflect"
	"testing"
	"time"
)

func TestArgVectors(t *testing.T) {
	g := SiteGemini
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"version", VersionArgs(), []string{"--version"}},
		{"doctor", DoctorArgs("p1"), []string{"--profile", "p1", "doctor"}},
		{"ask minimal", g.AskArgs("p1", AskOpts{Prompt: "你好"}), []string{"--profile", "p1", "gemini", "ask", "你好", "--format", "json"}},
		{
			"ask full",
			g.AskArgs("p1", AskOpts{Prompt: "pi", New: true, Model: "2.5-flash", Thinking: "extended", Timeout: 90 * time.Second}),
			[]string{"--profile", "p1", "gemini", "ask", "pi", "--new", "true", "--model", "2.5-flash", "--thinking", "extended", "--timeout", "90", "--format", "json"},
		},
		{"ask thinking standard", g.AskArgs("p1", AskOpts{Prompt: "x", Thinking: "standard"}), []string{"--profile", "p1", "gemini", "ask", "x", "--thinking", "standard", "--format", "json"}},
		{"ask no thinking flag", g.AskArgs("p1", AskOpts{Prompt: "x"}), []string{"--profile", "p1", "gemini", "ask", "x", "--format", "json"}},
		{"models", g.ModelsArgs("p1"), []string{"--profile", "p1", "gemini", "models", "--format", "json"}},
		{"status", g.StatusArgs("p1"), []string{"--profile", "p1", "gemini", "status", "--format", "json"}},
		{"detail", g.DetailArgs("p1", "abc123"), []string{"--profile", "p1", "gemini", "detail", "abc123", "--format", "json"}},
		{"whoami", g.WhoamiArgs("p1"), []string{"--profile", "p1", "gemini", "whoami", "--format", "json"}},
		{"login", g.LoginArgs("p1"), []string{"--profile", "p1", "gemini", "login", "--format", "json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !reflect.DeepEqual(c.got, c.want) {
				t.Fatalf("argv = %q, want %q", c.got, c.want)
			}
		})
	}
}

func TestAskOptsValidate(t *testing.T) {
	for _, v := range []string{"", ThinkingStandard, ThinkingExtended} {
		if err := (AskOpts{Prompt: "x", Thinking: v}).Validate(SiteGemini); err != nil {
			t.Fatalf("gemini thinking %q should validate: %v", v, err)
		}
	}
	for _, v := range []string{"bogus", "Standard", "EXTENDED", "null"} {
		if err := (AskOpts{Prompt: "x", Thinking: v}).Validate(SiteGemini); err == nil {
			t.Fatalf("gemini thinking %q must be rejected", v)
		}
	}
	// plain asks always validate on the supported site
	if err := (AskOpts{Prompt: "x"}).Validate(SiteGemini); err != nil {
		t.Fatalf("plain ask should validate: %v", err)
	}
}

func TestChildEnvAllowlist(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/svc",
		"TMPDIR=/tmp",
		"NODE_OPTIONS=--require evil.js",
		"NODE_PATH=/opt/evil",
		"LD_PRELOAD=/tmp/x.so",
	}
	got := ChildEnv(parent)
	want := []string{"PATH=/usr/bin:/bin", "HOME=/home/svc", "TMPDIR=/tmp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChildEnv = %q, want %q", got, want)
	}
}

func TestChildEnvSkipsMissingVars(t *testing.T) {
	got := ChildEnv([]string{"PATH=/usr/bin", "NODE_OPTIONS=x"})
	want := []string{"PATH=/usr/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ChildEnv = %q, want %q", got, want)
	}
}

func TestParseConversationID(t *testing.T) {
	cases := []struct {
		site *Site
		in   string
		want string
	}{
		{SiteGemini, "https://gemini.google.com/app/b8368a89d4242e5f", "b8368a89d4242e5f"},
		{SiteGemini, "https://gemini.google.com/app/b8368a89d4242e5f?hl=zh-CN", "b8368a89d4242e5f"},
		{SiteGemini, "/app/b8368a89d4242e5f", "b8368a89d4242e5f"},
		{SiteGemini, "b8368a89d4242e5f", "b8368a89d4242e5f"},
		{SiteGemini, "https://gemini.google.com/app/", ""},
		{SiteGemini, "https://gemini.google.com/", ""},
		{SiteGemini, "", ""},
		{SiteGemini, "not a conversation", ""},
	}
	for _, c := range cases {
		if got := c.site.ConversationID(c.in); got != c.want {
			t.Fatalf("%s ConversationID(%q) = %q, want %q", c.site.Name, c.in, got, c.want)
		}
	}
}

func TestParseStatusURLAndResumeCheck(t *testing.T) {
	const id = "b8368a89d4242e5f"
	out := `[{"Status":"Connected","Login":"Yes","Url":"https://gemini.google.com/app/` + id + `"}]`
	if got := ParseStatusURL(out); got != "https://gemini.google.com/app/"+id {
		t.Fatalf("ParseStatusURL = %q", got)
	}
	if !SiteGemini.StatusURLHasConversationID(out, id) {
		t.Fatal("status URL must match the target conversation id")
	}
	if SiteGemini.StatusURLHasConversationID(out, "deadbeefdeadbeef") {
		t.Fatal("status URL must not match a different conversation id")
	}
	if SiteGemini.StatusURLHasConversationID("not json", id) {
		t.Fatal("unparseable status must never verify")
	}
	// legacy bare-object shape
	if got := ParseStatusURL(`{"Url":"/app/` + id + `"}`); got != "/app/"+id {
		t.Fatalf("ParseStatusURL bare object = %q", got)
	}
}
