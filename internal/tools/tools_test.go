package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recorded struct{ method, path, body string }

// harness runs the native tools against a fake Erghi API through a real MCP
// client/server pair.
type harness struct {
	session *mcp.ClientSession
	mu      sync.Mutex
	calls   []recorded
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	h := &harness{}
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(map[string]any{"sub": "cf_client_live_x", "workspace_id": "ws-1", "permission": opts.Scopes})
	token := enc([]byte(`{}`)) + "." + enc(payload) + ".sig"
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_in": 7200})
			return
		}
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.calls = append(h.calls, recorded{r.Method, r.URL.RequestURI(), string(body)})
		h.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(api.Close)

	client, err := erghi.New(erghi.Config{BaseURL: api.URL, ClientID: "cf_client_live_x", ClientSecret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.WidgetHost == "" {
		opts.WidgetHost = "https://chat.erghi.ai"
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	Register(server, client, opts)
	RegisterPrompts(server)

	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	h.session, err = mcp.NewClient(&mcp.Implementation{Name: "c"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.session.Close() })
	return h
}

func (h *harness) toolNames(t *testing.T) []string {
	t.Helper()
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func (h *harness) call(t *testing.T, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	return res, text
}

func (h *harness) recorded() []recorded {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

func TestToolsFollowKeyScopes(t *testing.T) {
	names := newHarness(t, Options{Scopes: []string{"manage_widgets"}}).toolNames(t)
	for _, want := range []string{"get_api_key_info", "create_widget", "get_widget_embed_code"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"update_ai_settings", "get_branding", "list_audit_logs", "crawl_website"} {
		if slices.Contains(names, unwanted) {
			t.Errorf("%s offered without its scope", unwanted)
		}
	}

	// Branding is reachable with either of its two scopes.
	names = newHarness(t, Options{Scopes: []string{"manage_workspace_identity"}}).toolNames(t)
	if !slices.Contains(names, "update_branding") || slices.Contains(names, "update_ai_settings") {
		t.Errorf("identity-only tools = %v", names)
	}
}

func TestReadOnlyOffersNoWrites(t *testing.T) {
	h := newHarness(t, Options{ReadOnly: true, Scopes: []string{"configure_ai", "manage_workspace_identity", "manage_widgets", "view_audit_logs"}})
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) < 10 {
		t.Fatalf("only %d read tools", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s offered in read-only mode", tool.Name)
		}
	}
}

func TestDestructiveToolsNeedConfirm(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "manage_widgets"}})
	docID := "8f7c2a52-3a8e-4bb4-9d1d-6f0b6a0c1e11"

	res, text := h.call(t, "delete_knowledge_document", map[string]any{"documentId": docID, "confirm": false})
	if !res.IsError || !strings.Contains(text, "confirm=true") || len(h.recorded()) != 0 {
		t.Fatalf("unconfirmed delete: isError=%v text=%q calls=%v", res.IsError, text, h.recorded())
	}

	res, _ = h.call(t, "delete_knowledge_document", map[string]any{"documentId": docID, "confirm": true})
	got := h.recorded()
	if res.IsError || len(got) != 1 || got[0].method != http.MethodDelete || got[0].path != "/api/v1/rag/documents/"+docID {
		t.Fatalf("confirmed delete: isError=%v calls=%v", res.IsError, got)
	}
}

func TestIDsCannotSteerThePath(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_widgets", "configure_ai"}})
	for _, bad := range []string{"../billing", "x/../../admin", "", "8f7c2a52-3a8e-4bb4-9d1d-6f0b6a0c1e11/../x"} {
		res, _ := h.call(t, "get_widget", map[string]any{"widgetId": bad})
		if !res.IsError {
			t.Errorf("widgetId %q accepted", bad)
		}
	}
	res, _ := h.call(t, "get_channel_status", map[string]any{"channel": "../admin"})
	if !res.IsError {
		t.Error("unknown channel accepted")
	}
	if calls := h.recorded(); len(calls) != 0 {
		t.Errorf("requests sent: %v", calls)
	}
}

func TestQueryAndBodyAreEncoded(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "view_audit_logs"}})
	h.call(t, "search_knowledge_base", map[string]any{"query": "refund & returns?"})
	h.call(t, "list_audit_logs", map[string]any{"limit": 5000})
	h.call(t, "update_ai_settings", map[string]any{"preferredModel": "m"})
	got := h.recorded()
	if got[0].path != "/api/v1/rag/search?query=refund+%26+returns%3F" {
		t.Errorf("search path = %s", got[0].path)
	}
	if !strings.Contains(got[1].path, "limit=200") {
		t.Errorf("audit limit not capped: %s", got[1].path)
	}
	if got[2].method != http.MethodPut || got[2].body != `{"preferredModel":"m"}` {
		t.Errorf("update body = %s %s", got[2].method, got[2].body)
	}
}

func TestEmbedCode(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_widgets"}, WidgetHost: "https://chat.example"})
	_, text := h.call(t, "get_widget_embed_code", map[string]any{"widgetId": "8f7c2a52-3a8e-4bb4-9d1d-6f0b6a0c1e11"})
	if !strings.Contains(text, `src="https://chat.example/widget.js"`) || !strings.Contains(text, `data-widget-id="8f7c2a52-3a8e-4bb4-9d1d-6f0b6a0c1e11"`) {
		t.Errorf("embed = %s", text)
	}
}

func TestPrompts(t *testing.T) {
	h := newHarness(t, Options{})
	res, err := h.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name: "setup_erghi", Arguments: map[string]string{"website": "https://acme.test", "company": "Acme"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{"Acme", "https://acme.test", "get_api_key_info", "confirm=true"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}
