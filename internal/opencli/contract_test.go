package opencli

import (
	"testing"
)

func TestAskOutcomeMatrix(t *testing.T) {
	cases := []struct {
		name       string
		r          Result
		want       Outcome
		wantReason string
	}{
		{"spawn failure is failed", Result{Started: false}, OutcomeFailed, "spawn"},
		{"exit 77 is auth_required", Result{Started: true, ExitCode: 77}, OutcomeAuthRequired, "exit 77"},
		{"exit 77 wins over sentinel", Result{Started: true, ExitCode: 77, Stdout: SentinelPrefix + " No Gemini response within 60s"}, OutcomeAuthRequired, "exit 77"},
		{"exit 0 valid json is success", Result{Started: true, ExitCode: 0, Stdout: `{"response":"ok"}`}, OutcomeSuccess, ""},
		{"exit 0 response keeps sentinel text inside body", Result{Started: true, ExitCode: 0, Stdout: `{"response":"see 💬 [NO RESPONSE] note"}`}, OutcomeSuccess, ""},
		{"sentinel with exit 0 is unknown", Result{Started: true, ExitCode: 0, Stdout: SentinelPrefix + " No Gemini response within 60s"}, OutcomeUnknown, "sentinel"},
		{"invalid json is unknown", Result{Started: true, ExitCode: 0, Stdout: "not json"}, OutcomeUnknown, "bad_json"},
		{"json missing response field is unknown", Result{Started: true, ExitCode: 0, Stdout: `{}`}, OutcomeUnknown, "bad_json"},
		{"empty stdout is unknown", Result{Started: true, ExitCode: 0, Stdout: ""}, OutcomeUnknown, "bad_json"},
		{"stdout overflow is unknown even with valid json", Result{Started: true, ExitCode: 0, Stdout: `{"response":"ok"}`, Overflow: "stdout"}, OutcomeUnknown, "overflow:stdout"},
		{"timeout kill is unknown", Result{Started: true, ExitCode: -1, TimedOut: true}, OutcomeUnknown, "timeout"},
		{"cancel kill is unknown", Result{Started: true, ExitCode: -1, Canceled: true}, OutcomeUnknown, "canceled"},
		{"exit 2 is unknown", Result{Started: true, ExitCode: 2}, OutcomeUnknown, "exit 2"},
		{"exit 66 is unknown", Result{Started: true, ExitCode: 66}, OutcomeUnknown, "exit 66"},
		{"exit 69 is unknown", Result{Started: true, ExitCode: 69}, OutcomeUnknown, "exit 69"},
		{"exit 75 is unknown", Result{Started: true, ExitCode: 75}, OutcomeUnknown, "exit 75"},
		{"exit 78 is unknown", Result{Started: true, ExitCode: 78}, OutcomeUnknown, "exit 78"},
		{"exit 130 is unknown", Result{Started: true, ExitCode: 130}, OutcomeUnknown, "exit 130"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := AskOutcomeOf(c.r)
			if got != c.want || reason != c.wantReason {
				t.Fatalf("AskOutcomeOf(%+v) = %s (%s), want %s (%s)", c.r, got, reason, c.want, c.wantReason)
			}
		})
	}
}

func TestSentinelPrecision(t *testing.T) {
	cases := []struct {
		stdout string
		want   bool
	}{
		{SentinelPrefix + " No Gemini response within 60s", true},
		{"[NO RESPONSE] No Gemini response within 60s", false},
		{"note: " + SentinelPrefix + " mid-text", false},
		{`{"response":"` + SentinelPrefix + `"}"...`, false},
		{"plain markdown body", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsSentinel(c.stdout); got != c.want {
			t.Fatalf("IsSentinel(%q) = %v, want %v", c.stdout, got, c.want)
		}
	}
}

func TestParseAsk(t *testing.T) {
	if r, err := ParseAsk(`{"response":"hello"}`); err != nil || r.Response != "hello" {
		t.Fatalf("parse hello: %+v %v", r, err)
	}
	if r, err := ParseAsk(`{"response":""}`); err != nil || r.Response != "" {
		t.Fatalf("parse empty response as present field: %+v %v", r, err)
	}
	if _, err := ParseAsk(`{}`); err == nil {
		t.Fatal("missing response field must not parse as success")
	}
	if _, err := ParseAsk(`{"foo":1}`); err == nil {
		t.Fatal("foreign json must not parse as success")
	}
	if _, err := ParseAsk("not json at all"); err == nil {
		t.Fatal("invalid json must error")
	}
}
