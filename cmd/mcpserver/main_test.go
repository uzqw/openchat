package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

var convDetail = conversationDetail{
	conversation: conversation{ID: "c1", Title: "部署排查", Status: "active", Created: "2025-06-01T10:00:00Z"},
	Turns: []turn{
		{
			ID:      "t1",
			Prompt:  "caddy 重启后 502",
			Created: "2025-06-01T10:01:00Z",
			CurrentTask: &task{
				Status:        "succeeded",
				ResolvedModel: "gemini-2.5-pro",
				LatencyMS:     8200,
				Result:        "检查 upstream 未启动: systemctl status openchat-server",
			},
		},
		{
			ID:      "t2",
			Prompt:  "如何加 MCP",
			Created: "2025-06-01T10:05:00Z",
			CurrentTask: &task{
				Status:       "failed",
				ErrorMessage: "OpenCLI 版本被守卫拒绝",
			},
		},
	},
}

func TestRenderConversation(t *testing.T) {
	out := renderConversation(&convDetail)
	for _, want := range []string{
		"# 部署排查",
		"## Turn 1",
		"Q: caddy 重启后 502",
		"A: 检查 upstream 未启动",
		"A: [failed] OpenCLI 版本被守卫拒绝",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q\n%s", want, out)
		}
	}
}

func call(t *testing.T, c *client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	var out *mcp.CallToolResult
	var err error
	switch name {
	case "list_conversations":
		out, err = c.handleListConversations(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	case "get_conversation":
		out, err = c.handleGetConversation(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	}
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return out
}

func resultText(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestToolsEndToEnd(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/conversations":
			json.NewEncoder(w).Encode(listResult{Items: []conversation{convDetail.conversation}, Page: 1, Total: 1, LastPage: 1})
		case r.URL.Path == "/api/conversations/c1":
			json.NewEncoder(w).Encode(convDetail)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	c := &client{base: fake.URL, hc: fake.Client()}

	list := call(t, c, "list_conversations", map[string]any{"limit": 10.0})
	if !strings.Contains(resultText(list), "c1") {
		t.Errorf("list result missing conversation: %s", resultText(list))
	}

	conv := call(t, c, "get_conversation", map[string]any{"id": "c1"})
	if !strings.Contains(resultText(conv), "部署排查") || !strings.Contains(resultText(conv), "Q: caddy 重启后 502") {
		t.Errorf("conversation transcript wrong: %s", resultText(conv))
	}

	missing := call(t, c, "get_conversation", map[string]any{"id": "nope"})
	if !strings.Contains(resultText(missing), "error:") {
		t.Errorf("missing conversation should surface error, got: %s", resultText(missing))
	}
}
