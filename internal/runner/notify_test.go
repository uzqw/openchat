package runner

import (
	"encoding/json"
	"testing"
)

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"你好世界你好世界你好世界", 10, "你好世界你好世界你好"}, // runes, not bytes
		{"short answer", 15, "short answer"},
		{"", 10, ""},
		{"abc", 2, "ab"},
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, c.n); got != c.want {
			t.Fatalf("truncateRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestSplitPublishURL(t *testing.T) {
	cases := []struct {
		in      string
		root    string
		topic   string
		wantErr bool
	}{
		{"https://ntfy.sh/your-topic-name", "https://ntfy.sh/", "your-topic-name", false},
		{"https://ntfy.example.com/my/sub/topic", "https://ntfy.example.com/", "my/sub/topic", false},
		{"", "", "", true},
		{"not-a-url", "", "", true},
		{"https://ntfy.sh/", "", "", true}, // no topic segment
	}
	for _, c := range cases {
		root, topic, err := splitPublishURL(c.in)
		if (err != nil) != c.wantErr {
			t.Fatalf("splitPublishURL(%q) err = %v, wantErr %v", c.in, err, c.wantErr)
		}
		if err == nil && (root != c.root || topic != c.topic) {
			t.Fatalf("splitPublishURL(%q) = (%q, %q), want (%q, %q)", c.in, root, topic, c.root, c.topic)
		}
	}
}

func TestNtfyPayload(t *testing.T) {
	body, err := ntfyPayload("your-topic-name",
		"帮我对比一下 n8n 和 Temporal",
		"n8n 是低代码工作流平台，Temporal 是持久化工作流引擎，两者定位不同……")
	if err != nil {
		t.Fatalf("ntfyPayload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("payload is not JSON: %v; body %s", err, body)
	}
	if m["topic"] != "your-topic-name" {
		t.Fatalf("topic = %v", m["topic"])
	}
	if m["title"] != "帮我对比一下 n8n" { // 10 runes
		t.Fatalf("title = %v, want 10-rune truncation", m["title"])
	}
	if m["message"] != "n8n 是低代码工作流平台，T" { // 15 runes
		t.Fatalf("message = %v, want 15-rune truncation", m["message"])
	}
	tags, ok := m["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "speech_balloon" {
		t.Fatalf("tags = %v, want [speech_balloon]", m["tags"])
	}
}

func TestNtfyPayloadStripsDisplayWrapper(t *testing.T) {
	body, err := ntfyPayload("t", "提问", "💬 分布式系统是由多台通过网络连接的计算机组成的")
	if err != nil {
		t.Fatalf("ntfyPayload: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if m["message"] != "分布式系统是由多台通过网络连接" { // 💬 wrapper stripped, then 15 runes
		t.Fatalf("message = %v, want strip+truncate", m["message"])
	}
}
