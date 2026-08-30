package opencli

import (
	"reflect"
	"testing"
	"time"
)

func TestArgVectors(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"version", VersionArgs(), []string{"--version"}},
		{"doctor", DoctorArgs("p1"), []string{"--profile", "p1", "doctor"}},
		{"ask minimal", AskArgs("p1", AskOpts{Prompt: "你好"}), []string{"--profile", "p1", "gemini", "ask", "你好", "--format", "json"}},
		{
			"ask full",
			AskArgs("p1", AskOpts{Prompt: "pi", New: true, Model: "2.5-flash", Thinking: "extended", Timeout: 90 * time.Second}),
			[]string{"--profile", "p1", "gemini", "ask", "pi", "--new", "true", "--model", "2.5-flash", "--thinking", "extended", "--timeout", "90", "--format", "json"},
		},
		{"ask thinking standard", AskArgs("p1", AskOpts{Prompt: "x", Thinking: "standard"}), []string{"--profile", "p1", "gemini", "ask", "x", "--thinking", "standard", "--format", "json"}},
		{"ask no thinking flag", AskArgs("p1", AskOpts{Prompt: "x"}), []string{"--profile", "p1", "gemini", "ask", "x", "--format", "json"}},
		{"models", ModelsArgs("p1"), []string{"--profile", "p1", "gemini", "models", "--format", "json"}},
		{"status", StatusArgs("p1"), []string{"--profile", "p1", "gemini", "status", "--format", "json"}},
		{"whoami", WhoamiArgs("p1"), []string{"--profile", "p1", "gemini", "whoami", "--format", "json"}},
		{"login", LoginArgs("p1"), []string{"--profile", "p1", "gemini", "login", "--format", "json"}},
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
	valid := []string{"", ThinkingStandard, ThinkingExtended}
	for _, v := range valid {
		if err := (AskOpts{Prompt: "x", Thinking: v}).Validate(); err != nil {
			t.Fatalf("thinking %q should validate: %v", v, err)
		}
	}
	for _, v := range []string{"bogus", "Standard", "EXTENDED", "null"} {
		if err := (AskOpts{Prompt: "x", Thinking: v}).Validate(); err == nil {
			t.Fatalf("thinking %q must be rejected", v)
		}
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
