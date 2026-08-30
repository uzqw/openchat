package provider

import (
	"testing"
	"time"
)

func TestParseLoggedIn(t *testing.T) {
	cases := []struct {
		out   string
		value bool
		known bool
	}{
		{`{"logged_in":true}`, true, true},
		{`{"loggedIn":false}`, false, true},
		{`{"is_logged_in":true}`, true, true},
		{`{"logged_in":"yes"}`, false, false}, // non-bool → unknown, never claim
		{`{"email":"a@b.c"}`, false, false},   // unknown shape → unknown
		{`not-json`, false, false},
		{``, false, false},
	}
	for _, tc := range cases {
		v, known := parseLoggedIn(tc.out)
		if v != tc.value || known != tc.known {
			t.Errorf("parseLoggedIn(%q) = (%v,%v), want (%v,%v)", tc.out, v, known, tc.value, tc.known)
		}
	}
}

func TestParseModels(t *testing.T) {
	cases := []struct {
		out  string
		want []string
	}{
		{`{"models":["2.5-flash","2.5-pro"]}`, []string{"2.5-flash", "2.5-pro"}},
		{`{"models":[{"id":"a"},{"id":"b"}]}`, []string{"a", "b"}},
		{`{"models":[]}`, nil},
		{`{"other":1}`, nil},
		{`garbage`, nil},
	}
	for _, tc := range cases {
		got := parseModels(tc.out)
		if len(got) != len(tc.want) {
			t.Errorf("parseModels(%q) = %v, want %v", tc.out, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseModels(%q) = %v, want %v", tc.out, got, tc.want)
				break
			}
		}
	}
}

func TestCacheStaleness(t *testing.T) {
	c := &Cache{loginOp: LoginOpIdle}
	if !c.stale(DefaultTTL) {
		t.Fatalf("a never-refreshed cache must be stale")
	}
	c.markRefreshed()
	if c.stale(time.Hour) {
		t.Fatalf("a just-refreshed cache must be fresh for a 1h TTL")
	}
	if !c.stale(-time.Second) {
		t.Fatalf("a negative TTL must always be stale")
	}
}
