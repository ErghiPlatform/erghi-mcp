// Package tools registers the Erghi tools on an MCP server.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options control which tools are offered.
type Options struct {
	// ReadOnly offers only tools that change nothing.
	ReadOnly bool
	// Scopes are the key's scopes. A tool is offered only when the key holds
	// one of the scopes it needs, so the model never sees tools that would
	// just fail. The API enforces scopes regardless.
	Scopes []string
	// WidgetHost serves widget.js, for embed snippets.
	WidgetHost string
	// AppURL is the admin app, linked for steps API keys can't do.
	AppURL string
	Logger *slog.Logger
}

type kind int

const (
	read kind = iota
	write
	destructive
)

// errNotConfirmed is returned by destructive tools called without confirm=true.
var errNotConfirmed = errors.New("this action removes or disables something; ask the user to confirm, then call again with confirm=true")

type registry struct {
	server *mcp.Server
	api    *erghi.Client
	opts   Options
	added  []string
}

// Register adds every native tool the key can use and returns their names.
func Register(server *mcp.Server, api *erghi.Client, opts Options) []string {
	r := &registry{server: server, api: api, opts: opts}
	registerWorkspace(r)
	registerAI(r)
	registerKnowledge(r)
	registerBranding(r)
	registerPolicies(r)
	registerChannels(r)
	registerWidgets(r)
	registerIntegrations(r)
	registerReview(r)
	registerGuide(r)
	registerAudit(r)
	registerInsights(r)
	return r.added
}

func (r *registry) hasAny(scopes []string) bool {
	if len(scopes) == 0 {
		return true
	}
	return slices.ContainsFunc(scopes, func(s string) bool { return slices.Contains(r.opts.Scopes, s) })
}

// add registers a typed tool. scopes lists the scopes that unlock it (any
// one is enough); nil means any key may call it.
func add[In any](r *registry, k kind, scopes []string, t *mcp.Tool, h func(context.Context, In) (any, error)) {
	if r.opts.ReadOnly && k != read {
		return
	}
	if !r.hasAny(scopes) {
		return
	}
	readOnly := k == read
	isDestructive := k == destructive
	t.Annotations = &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: &isDestructive,
		OpenWorldHint:   boolPtr(false),
	}
	mcp.AddTool(r.server, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		out, err := h(ctx, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return jsonResult(out), nil, nil
	})
	r.added = append(r.added, t.Name)
}

func boolPtr(b bool) *bool { return &b }

func jsonResult(v any) *mcp.CallToolResult {
	var text string
	switch x := v.(type) {
	case json.RawMessage:
		var pretty any
		if json.Unmarshal(x, &pretty) == nil {
			b, _ := json.MarshalIndent(pretty, "", "  ")
			text = string(b)
		} else {
			text = string(x)
		}
	case string:
		text = x
	default:
		b, err := json.MarshalIndent(x, "", "  ")
		if err != nil {
			return errorResult(err)
		}
		text = string(b)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errorResult(err error) *mcp.CallToolResult {
	msg := err.Error()
	var apiErr *erghi.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 403 {
		msg += " — the API key may be missing the scope this action needs; ask the user to create a key with it in the Erghi admin app (Settings → Agent API keys)."
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

func requireConfirm(confirm bool) error {
	if !confirm {
		return errNotConfirmed
	}
	return nil
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// id validates a resource id before it goes into a URL path, so a model can't
// steer a request to another endpoint with something like "../billing".
func id(name, v string) (string, error) {
	if !uuidPattern.MatchString(v) {
		return "", fmt.Errorf("%s must be a UUID, got %q", name, v)
	}
	return v, nil
}
