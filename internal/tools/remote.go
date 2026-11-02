package tools

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// remoteReadOnly are the Erghi-hosted MCP tools offered in read-only mode.
var remoteReadOnly = []string{"list_conversations", "get_conversation", "get_billing_plans"}

// remoteScopes are the scopes the hosted tools check on their side. A tool
// not listed here is offered to every key and left to the API to refuse.
var remoteScopes = map[string][]string{
	"list_conversations":   {"chat:read"},
	"get_conversation":     {"chat:read"},
	"send_reply":           {"chat:write"},
	"resolve_conversation": {"chat:write"},
}

// remote forwards calls to the MCP endpoint Erghi itself hosts at /api/mcp
// (conversations, replies, billing plans), so one server exposes both.
type remote struct {
	api      *erghi.Client
	version  string
	endpoint string
	log      *slog.Logger

	mu      sync.Mutex
	session *mcp.ClientSession
}

// RegisterRemote lists the tools Erghi's hosted MCP endpoint offers to this
// key and re-registers each one on server as a forwarding tool. Failing to
// reach it is not fatal: the native tools still work.
func RegisterRemote(ctx context.Context, server *mcp.Server, api *erghi.Client, opts Options, version string) []string {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	r := &remote{api: api, version: version, endpoint: api.BaseURL() + "/api/mcp", log: log}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	session, err := r.connect(ctx)
	if err != nil {
		log.Warn("Erghi-hosted MCP tools unavailable; conversation tools are not offered", "error", err)
		return nil
	}
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Warn("could not list Erghi-hosted MCP tools", "error", err)
		return nil
	}

	var added []string
	for _, t := range list.Tools {
		if opts.ReadOnly && !slices.Contains(remoteReadOnly, t.Name) {
			continue
		}
		if need := remoteScopes[t.Name]; len(need) > 0 &&
			!slices.ContainsFunc(need, func(s string) bool { return slices.Contains(opts.Scopes, s) }) {
			continue
		}
		tool := &mcp.Tool{
			Name:        t.Name,
			Title:       t.Title,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Annotations: t.Annotations,
		}
		if tool.InputSchema == nil {
			tool.InputSchema = map[string]any{"type": "object"}
		}
		name := t.Name
		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args any = map[string]any{}
			if raw := req.Params.Arguments; len(raw) > 0 && string(raw) != "null" {
				args = raw
			}
			return r.call(ctx, name, args), nil
		})
		added = append(added, name)
	}
	return added
}

func (r *remote) connect(ctx context.Context) (*mcp.ClientSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		return r.session, nil
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "erghi-mcp-proxy", Version: r.version}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: r.endpoint,
		HTTPClient: &http.Client{
			Timeout:   2 * time.Minute,
			Transport: &erghi.AuthTransport{Client: r.api},
		},
		MaxRetries:           2,
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", r.endpoint, err)
	}
	r.session = session
	return session, nil
}

func (r *remote) drop(stale *mcp.ClientSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == stale {
		_ = stale.Close()
		r.session = nil
	}
}

// call forwards one tool call, reconnecting once if the session went away
// (for example after an Erghi deploy).
func (r *remote) call(ctx context.Context, name string, args any) *mcp.CallToolResult {
	var lastErr error
	for range 2 {
		session, err := r.connect(ctx)
		if err != nil {
			lastErr = err
			continue
		}
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err == nil {
			return res
		}
		lastErr = err
		r.drop(session)
	}
	return errorResult(fmt.Errorf("%s: %w", name, lastErr))
}
