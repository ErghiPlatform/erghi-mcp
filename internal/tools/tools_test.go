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
		switch r.URL.Path {
		case "/api/widgets":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v1/rag/documents":
			_, _ = w.Write([]byte(`[{"id":"a"},{"id":"b"}]`))
		case "/api/v1/branding":
			_, _ = w.Write([]byte(`{"aiAssistantName":"Nour","aiSystemPrompt":"Help Acme customers."}`))
		case "/api/v1/integrations":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(integrationsFixture))
			} else {
				_, _ = w.Write([]byte(`{"ok":true}`))
			}
		case "/api/v1/admin/action-policies":
			_, _ = w.Write([]byte(policiesFixture))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
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
	if opts.AppURL == "" {
		opts.AppURL = "https://app.test"
	}
	Register(server, client, opts)
	RegisterChecklist(server, client, opts, nil)
	RegisterPrompts(server, opts.AppURL)

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
	for _, want := range []string{"Acme", "https://acme.test", "get_setup_checklist", "https://app.test/users", "confirm=true"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestSetupChecklistMirrorsDashboard(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "manage_widgets"}})
	res, text := h.call(t, "get_setup_checklist", nil)
	if res.IsError {
		t.Fatal(text)
	}
	var c checklist
	if err := json.Unmarshal([]byte(text), &c); err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, s := range c.Steps {
		status[s.Key] = s.Status
	}
	want := map[string]string{
		"assistant": "done", "knowledge": "done", "widget": "todo",
		"teammate": "unknown", "conversation": "unknown",
	}
	for k, v := range want {
		if status[k] != v {
			t.Errorf("%s = %q, want %q", k, status[k], v)
		}
	}
	if c.Done != 2 || c.Total != 5 || !strings.Contains(text, "https://app.test/users") {
		t.Errorf("checklist = %+v", c)
	}
}

const integrationID = "1b4e28ba-2fa1-11d2-883f-0016d3cca427"

// integrationsFixture is GET /api/v1/integrations?includeEndpoints=true for one
// integration with a literal credential binding, an unbound user id and an
// unprotected write.
const integrationsFixture = `[{
  "id": "` + integrationID + `", "name": "Orders", "baseUrl": "https://api.acme.test", "isActive": true, "endpointCount": 2,
  "requestBindings": [{"in": "header", "name": "Authorization", "value": "Bearer abc123", "required": true}],
  "aiInstructions": null,
  "endpoints": [
    {"id": "6a1f0d1e-0000-4000-8000-000000000001", "operationId": "getOrders", "method": "GET", "path": "/users/{user_id}/orders",
     "parametersJson": "[{\"Name\":\"user_id\",\"In\":\"path\",\"Required\":true}]", "requestBindings": []},
    {"id": "6a1f0d1e-0000-4000-8000-000000000002", "operationId": "cancelOrder", "method": "POST", "path": "/orders/{id}/cancel",
     "parametersJson": "[{\"Name\":\"id\",\"In\":\"path\",\"Required\":true}]", "requestBindings": []}
  ]
}]`

func TestIntegrationToolsFollowTheirScope(t *testing.T) {
	names := newHarness(t, Options{Scopes: []string{"manage_integrations"}}).toolNames(t)
	for _, want := range []string{"list_integrations", "add_integration_from_openapi", "set_integration_tool_context", "test_integration_endpoint", "delete_integration", "review_chat_setup", "get_erghi_guide"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s", want)
		}
	}
	names = newHarness(t, Options{Scopes: []string{"configure_ai"}}).toolNames(t)
	if slices.Contains(names, "list_integrations") || !slices.Contains(names, "create_action_policy") {
		t.Errorf("configure_ai tools = %v", names)
	}
}

func TestAddIntegrationUploadsTheSpec(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_integrations"}})
	res, text := h.call(t, "add_integration_from_openapi", map[string]any{
		"name": "Orders", "baseUrl": "https://api.acme.test", "specContent": `{"openapi":"3.0.0","paths":{}}`,
	})
	got := h.recorded()
	if res.IsError || len(got) != 1 || got[0].method != http.MethodPost || got[0].path != "/api/v1/integrations/upload-spec" {
		t.Fatalf("upload: %s %v", text, got)
	}
	for _, want := range []string{`name="specFile"; filename="openapi.json"`, `name="integrationName"`, "Orders", `{"openapi":"3.0.0","paths":{}}`} {
		if !strings.Contains(got[0].body, want) {
			t.Errorf("multipart body missing %q", want)
		}
	}
}

func TestToolContextTargetsTheEndpoint(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_integrations"}})
	endpointID := "6a1f0d1e-0000-4000-8000-000000000001"
	h.call(t, "set_integration_tool_context", map[string]any{"integrationId": integrationID, "endpointId": endpointID})
	got := h.recorded()
	if len(got) != 1 || got[0].method != http.MethodPut ||
		got[0].path != "/api/v1/integrations/"+integrationID+"/endpoints/"+endpointID+"/tool-context" ||
		got[0].body != `{"aiInstructions":null,"requestBindings":[]}` {
		t.Fatalf("tool context call = %v", got)
	}
}

func TestTestingAWriteEndpointNeedsConfirm(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_integrations"}})
	res, text := h.call(t, "test_integration_endpoint", map[string]any{"integrationId": integrationID, "operationId": "cancelOrder"})
	if !res.IsError || !strings.Contains(text, "confirm=true") {
		t.Fatalf("unconfirmed POST test: %v %s", res.IsError, text)
	}
	h.call(t, "test_integration_endpoint", map[string]any{"integrationId": integrationID, "operationId": "getOrders", "pathParameters": map[string]string{"user_id": "u1"}})
	var tests []recorded
	for _, c := range h.recorded() {
		if strings.HasSuffix(c.path, "/test") {
			tests = append(tests, c)
		}
	}
	if len(tests) != 1 || !strings.Contains(tests[0].body, `"operationId":"getOrders"`) || !strings.Contains(tests[0].body, `"useSavedCredentials":true`) {
		t.Fatalf("test calls = %v", tests)
	}
}

func TestReviewFindsIntegrationRisks(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "manage_integrations"}})
	res, text := h.call(t, "review_chat_setup", nil)
	if res.IsError {
		t.Fatal(text)
	}
	var rev chatSetupReview
	if err := json.Unmarshal([]byte(text), &rev); err != nil {
		t.Fatal(err)
	}
	has := func(sev, substr string) bool {
		return slices.ContainsFunc(rev.Findings, func(f finding) bool { return f.Severity == sev && strings.Contains(f.Issue+f.Area, substr) })
	}
	if !has("high", `"user_id"`) {
		t.Error("unbound user_id not flagged")
	}
	if !has("high", "Authorization") {
		t.Error("literal Authorization binding not flagged")
	}
	if !has("medium", "cancelOrder") {
		t.Error("unprotected write not flagged")
	}
	if !has("high", `Step "verify_otp" can never complete`) {
		t.Error("a step with no usable prerequisite not flagged")
	}
	if has("high", `Step "verify_customer"`) {
		t.Error("a completable step was flagged")
	}
	if !has("low", `Step "verify_customer" completes on any successful`) {
		t.Error("a step with no response check not flagged")
	}
	if has("low", `Step "checked_customer"`) {
		t.Error("a step with a response check was flagged")
	}
	if has("high", `"id"`) {
		t.Error("a plain resource id was flagged as a user id")
	}
	if rev.Findings[0].Severity != "high" || !slices.Contains(rev.NotChecked, "widgets (need manage_widgets)") {
		t.Errorf("review = %+v", rev)
	}
}

func TestGuide(t *testing.T) {
	h := newHarness(t, Options{})
	_, text := h.call(t, "get_erghi_guide", map[string]any{"topic": "integrations"})
	if !strings.Contains(text, "{{identity.sub}}") {
		t.Errorf("integrations guide = %.80s", text)
	}
	_, text = h.call(t, "get_erghi_guide", nil)
	for _, g := range guideTopics {
		if !strings.Contains(text, g.topic) {
			t.Errorf("topic list missing %s", g.topic)
		}
		if _, err := guide(g.topic); err != nil {
			t.Errorf("%s: %v", g.topic, err)
		}
	}
}

func TestAnalyzeProductPrompt(t *testing.T) {
	h := newHarness(t, Options{})
	res, err := h.session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name: "analyze_product", Arguments: map[string]string{"website": "https://acme.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{"https://acme.test", "review_chat_setup", "product_analysis", "https://app.test/integrations", "another customer's data"} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// One step that can complete (cancelOrder is an active operation the policy does not protect)
// and one that can't: its only prerequisite is not offered by any integration.
const policiesFixture = `[
  {"id": "p1", "policyKey": "orders_guard", "name": "Orders guard", "isActive": true,
   "protectedOperations": ["getOrders"],
   "requiredSteps": [
     {"key": "verify_customer", "prerequisiteOperationIds": ["cancelOrder"]},
     {"key": "verify_otp", "prerequisiteOperationIds": ["verifyOtp"]},
     {"key": "checked_customer", "prerequisiteOperationIds": ["cancelOrder"],
      "successWhen": {"path": "verified", "equals": "true"}}
   ]}
]`

// The policy tools used to take json.RawMessage, whose inferred schema is "null or array",
// so the SDK refused every policy object before the call reached Erghi.
func TestPolicyToolsAcceptAPolicyObject(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai"}})
	policy := map[string]any{
		"policyKey":           "refund_guard",
		"name":                "Refund guard",
		"protectedOperations": []string{"refundOrder"},
		"requiredSteps": []map[string]any{
			{"key": "verify_customer", "blockMessage": "Verify first.", "prerequisiteOperationIds": []string{"verifyCustomer"}},
		},
	}
	if res, text := h.call(t, "create_action_policy", map[string]any{"policy": policy}); res.IsError {
		t.Fatalf("create refused: %s", text)
	}
	pid := "3f1c2b9a-7d4e-4a8b-9c1d-2e3f4a5b6c7d"
	if res, text := h.call(t, "update_action_policy", map[string]any{"policyId": pid, "policy": policy}); res.IsError {
		t.Fatalf("update refused: %s", text)
	}
	if res, _ := h.call(t, "update_action_policy", map[string]any{"policyId": pid}); !res.IsError {
		t.Error("update without a policy should be refused")
	}
	got := h.recorded()
	if len(got) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(got))
	}
	if got[0].method != http.MethodPost || got[0].path != "/api/v1/admin/action-policies" ||
		!strings.Contains(got[0].body, `"protectedOperations":["refundOrder"]`) {
		t.Errorf("create sent %s %s %s", got[0].method, got[0].path, got[0].body)
	}
	if got[1].method != http.MethodPut || got[1].path != "/api/v1/admin/action-policies/"+pid {
		t.Errorf("update sent %s %s", got[1].method, got[1].path)
	}
}

func TestPolicyStatsAndDryRun(t *testing.T) {
	h := newHarness(t, Options{ReadOnly: true, Scopes: []string{"configure_ai"}})
	names := h.toolNames(t)
	for _, want := range []string{"get_action_policy_stats", "test_action_policy"} {
		if !slices.Contains(names, want) {
			t.Errorf("%s not offered to a read-only configure_ai key", want)
		}
	}
	if res, text := h.call(t, "get_action_policy_stats", map[string]any{}); res.IsError {
		t.Fatalf("stats refused: %s", text)
	}
	if res, _ := h.call(t, "get_action_policy_stats", map[string]any{"days": 91}); !res.IsError {
		t.Error("days=91 should be refused")
	}
	if res, _ := h.call(t, "test_action_policy", map[string]any{"operationId": " "}); !res.IsError {
		t.Error("a test without an operation id should be refused")
	}
	args := map[string]any{
		"operationId":      "refundOrder",
		"arguments":        map[string]any{"order_id": "1042"},
		"identityVerified": true,
		"completedSteps":   []map[string]any{{"key": "looked_up_order", "target": "1042"}},
	}
	if res, text := h.call(t, "test_action_policy", args); res.IsError {
		t.Fatalf("test refused: %s", text)
	}
	got := h.recorded()
	if len(got) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(got))
	}
	if got[0].method != http.MethodGet || got[0].path != "/api/v1/admin/action-policies/stats?days=7" {
		t.Errorf("stats sent %s %s", got[0].method, got[0].path)
	}
	if got[1].method != http.MethodPost || got[1].path != "/api/v1/admin/action-policies/test" ||
		!strings.Contains(got[1].body, `"completedSteps":[{"key":"looked_up_order","target":"1042"}]`) ||
		!strings.Contains(got[1].body, `"identityVerified":true`) {
		t.Errorf("test sent %s %s %s", got[1].method, got[1].path, got[1].body)
	}
}

func TestInsightToolsNeedTheirScope(t *testing.T) {
	names := newHarness(t, Options{Scopes: []string{"view_insights"}}).toolNames(t)
	for _, want := range []string{"get_insights_summary", "list_insight_conversations"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s", want)
		}
	}
	names = newHarness(t, Options{Scopes: []string{"chat:read", "view_audit_logs"}}).toolNames(t)
	if slices.Contains(names, "get_insights_summary") || slices.Contains(names, "list_insight_conversations") {
		t.Errorf("insight tools offered without view_insights: %v", names)
	}
	// Read-only tools stay in read-only mode.
	names = newHarness(t, Options{ReadOnly: true, Scopes: []string{"view_insights"}}).toolNames(t)
	if !slices.Contains(names, "get_insights_summary") {
		t.Errorf("read-only mode dropped get_insights_summary: %v", names)
	}
}

func TestInsightToolsBuildTheQuery(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"view_insights"}})
	h.call(t, "get_insights_summary", map[string]any{"from": "2026-11-01T00:00:00Z", "outcome": "resolved_by_ai", "minConfidence": 0.5})
	h.call(t, "get_insights_summary", map[string]any{})
	h.call(t, "list_insight_conversations", map[string]any{"topic": "refund & returns", "pageSize": 5000, "page": -3})
	h.call(t, "list_insight_conversations", map[string]any{})
	got := h.recorded()
	if got[0].method != http.MethodGet || got[0].path != "/api/v1/insights/summary?from=2026-11-01T00%3A00%3A00Z&minConfidence=0.5&outcome=resolved_by_ai" {
		t.Errorf("summary = %s %s", got[0].method, got[0].path)
	}
	if got[1].path != "/api/v1/insights/summary" {
		t.Errorf("unfiltered summary = %s", got[1].path)
	}
	if got[2].path != "/api/v1/insights/conversations?page=1&pageSize=100&topic=refund+%26+returns" {
		t.Errorf("list = %s", got[2].path)
	}
	if got[3].path != "/api/v1/insights/conversations?page=1&pageSize=25" {
		t.Errorf("default list = %s", got[3].path)
	}
}
