// Command mcpserver exposes OpenChat conversation history to external AI
// agents over MCP (stdio). It is a READ-ONLY bridge over the existing
// REST API (docs/domain-api.md §4): every tool maps to an existing GET
// endpoint and the responses are rendered as AI-friendly transcript text.
//
// Write endpoints are deliberately not exposed — the write path owns
// queue invariants and fail-closed guards (docs/deployment-operations.md
// §3) that an external agent must not bypass.
//
// Environment:
//
//	OPENCHAT_API_URL            base URL of the openchat-server API (default http://127.0.0.1:8090)
//	OPENCHAT_API_USER/PASS      optional Basic Auth for the API
//	OPENCHAT_API_TIMEOUT_SECONDS  HTTP timeout (default 120)
//
// Connect another AI agent (Claude Code, Cursor, ...) as a stdio MCP
// server with: command = mcpserver, args = [].
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const version = "0.1.0"

// ---- wire types mirroring internal/api/handlers.go (read-only subset) ----

type conversation struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Created string `json:"created"`
}

type task struct {
	Status         string `json:"status"`
	Result         string `json:"result"`
	ErrorCode      string `json:"error_code"`
	ErrorMessage   string `json:"error_message"`
	RequestedModel string `json:"requested_model"`
	ResolvedModel  string `json:"resolved_model"`
	Thinking       string `json:"thinking"`
	LatencyMS      int64  `json:"latency_ms"`
	Created        string `json:"created"`
}

type turn struct {
	ID          string `json:"id"`
	Prompt      string `json:"prompt"`
	Created     string `json:"created"`
	CurrentTask *task  `json:"current_task"`
}

type conversationDetail struct {
	conversation
	Turns []turn `json:"turns"`
}

type listResult struct {
	Items    []conversation `json:"items"`
	Page     int            `json:"page"`
	Total    int            `json:"totalItems"`
	LastPage int            `json:"totalPages"`
}

// ---- HTTP client for the openchat REST API ----

type client struct {
	base string
	user string
	pass string
	hc   *http.Client
}

func newClientFromEnv() *client {
	timeout := 120 * time.Second
	if v, err := strconv.Atoi(os.Getenv("OPENCHAT_API_TIMEOUT_SECONDS")); err == nil && v > 0 {
		timeout = time.Duration(v) * time.Second
	}
	base := os.Getenv("OPENCHAT_API_URL")
	if base == "" {
		base = "http://127.0.0.1:8090"
	}
	return &client{
		base: strings.TrimRight(base, "/"),
		user: os.Getenv("OPENCHAT_API_USER"),
		pass: os.Getenv("OPENCHAT_API_PASS"),
		hc:   &http.Client{Timeout: timeout},
	}
}

func (c *client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("openchat api unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return os.ErrNotExist
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("openchat api %s %s: %s", req.Method, path, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// ---- tool implementations ----

func (c *client) handleListConversations(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	page := request.GetInt(argPage, 1)
	limit := request.GetInt(argLimit, 50)
	if limit < 1 || limit > 200 {
		return mcp.NewToolResultText("limit must be between 1 and 200"), nil
	}
	var p listResult
	if err := c.get(ctx, fmt.Sprintf("/api/conversations?page=%d&perPage=%d", page, limit), &p); err != nil {
		return toolErr(err), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d 个会话（共 %d，第 %d/%d 页）\n\n", len(p.Items), p.Total, p.Page, p.LastPage)
	for _, cv := range p.Items {
		fmt.Fprintf(&b, "%s  %s  %s  %s\n", cv.ID, cv.Status, cv.Title, cv.Created)
	}
	return mcp.NewToolResultText(b.String()), nil
}

func (c *client) handleGetConversation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString(argID, "")
	if id == "" {
		return mcp.NewToolResultText("missing required argument: id"), nil
	}
	var d conversationDetail
	if err := c.get(ctx, "/api/conversations/"+id, &d); err != nil {
		return toolErr(err), nil
	}
	return mcp.NewToolResultText(renderConversation(&d)), nil
}

func (c *client) handleGetTurn(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString(argID, "")
	if id == "" {
		return mcp.NewToolResultText("missing required argument: id"), nil
	}
	var t turn
	if err := c.get(ctx, "/api/turns/"+id, &t); err != nil {
		return toolErr(err), nil
	}
	return mcp.NewToolResultText(renderTurn(&t, 1)), nil
}

// ---- rendering ----

// renderConversation turns one conversation into a complete transcript,
// prompts and answers, ready for an agent to read or summarize.
func renderConversation(d *conversationDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\nid=%s status=%s created=%s\n\n", d.Title, d.ID, d.Status, d.Created)
	for i := range d.Turns {
		b.WriteString(renderTurn(&d.Turns[i], i+1))
	}
	return b.String()
}

func renderTurn(t *turn, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Turn %d · %s\nQ: %s\n", n, t.Created, t.Prompt)
	if t.CurrentTask == nil {
		b.WriteString("A: (无结果)\n\n")
		return b.String()
	}
	tsk := t.CurrentTask
	if tsk.Status != "succeeded" && tsk.ErrorMessage != "" {
		fmt.Fprintf(&b, "A: [%s] %s\n\n", tsk.Status, tsk.ErrorMessage)
		return b.String()
	}
	fmt.Fprintf(&b, "model=%s thinking=%s latency_ms=%d\nA: %s\n\n", tsk.ResolvedModel, tsk.Thinking, tsk.LatencyMS, strings.TrimSpace(tsk.Result))
	return b.String()
}

// ---- MCP glue ----

func toolErr(err error) *mcp.CallToolResult {
	return mcp.NewToolResultText("error: " + err.Error())
}

func tool(name, desc string, opts ...mcp.ToolOption) mcp.Tool {
	opts = append(opts,
		mcp.WithDescription(desc),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)
	return mcp.NewTool(name, opts...)
}

const (
	argID    = "id"
	argPage  = "page"
	argLimit = "limit"
)

func main() {
	c := newClientFromEnv()
	srv := server.NewMCPServer("openchat-history", version)

	srv.AddTool(tool("list_conversations",
		"列出会话（按创建时间倒序，最新在前）。page 从 1 开始，limit 1-200。",
		mcp.WithNumber(argPage, mcp.Description("页码，从 1 开始"), mcp.DefaultNumber(1)),
		mcp.WithNumber(argLimit, mcp.Description("每页数量，1-200"), mcp.DefaultNumber(50)),
	), c.handleListConversations)

	srv.AddTool(tool("get_conversation",
		"取一个会话的完整转写（所有轮次的提问与回答），用于阅读或总结。",
		mcp.WithString(argID, mcp.Required(), mcp.Description("会话 id")),
	), c.handleGetConversation)

	srv.AddTool(tool("get_turn",
		"取单轮提问/回答详情。",
		mcp.WithString(argID, mcp.Required(), mcp.Description("turn id")),
	), c.handleGetTurn)

	server.ServeStdio(srv)
}
