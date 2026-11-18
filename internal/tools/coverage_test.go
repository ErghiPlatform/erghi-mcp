package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	uuidA = "11111111-1111-1111-1111-111111111111"
	uuidB = "22222222-2222-2222-2222-222222222222"
)

func TestNewToolsFollowTheirScope(t *testing.T) {
	cases := map[string][]string{
		"manage_inbox_settings": {"list_conversation_labels", "create_saved_reply", "create_team", "update_assignment_settings",
			"set_business_hours", "create_sla_policy", "create_automation", "update_csat_settings", "add_inbox_member"},
		"manage_widgets":            {"list_campaigns", "create_campaign", "delete_campaign"},
		"view_insights":             {"get_report_summary", "get_report_breakdown", "list_csat_responses"},
		"chat:read":                 {"list_conversation_messages"},
		"configure_ai":              {"get_voice_usage", "retry_document_embedding", "get_operation_fields", "connect_email"},
		"manage_workspace_identity": {"remove_logo", "set_custom_domain", "verify_custom_domain"},
	}
	all := map[string]bool{}
	for _, tools := range cases {
		for _, n := range tools {
			all[n] = true
		}
	}
	for scope, want := range cases {
		names := newHarness(t, Options{Scopes: []string{scope}}).toolNames(t)
		for _, n := range want {
			if !slices.Contains(names, n) {
				t.Errorf("scope %s: missing %s", scope, n)
			}
		}
		for n := range all {
			if !slices.Contains(want, n) && slices.Contains(names, n) {
				t.Errorf("scope %s: offers %s, which belongs to another scope", scope, n)
			}
		}
	}
}

func TestWebhooksNeedIntegrationsAndChatRead(t *testing.T) {
	webhookTools := []string{"list_webhooks", "create_webhook", "rotate_webhook_secret", "list_webhook_deliveries"}
	for _, tc := range []struct {
		scopes []string
		want   bool
	}{
		{[]string{"manage_integrations"}, false},
		{[]string{"chat:read"}, false},
		{[]string{"manage_integrations", "chat:read"}, true},
	} {
		names := newHarness(t, Options{Scopes: tc.scopes}).toolNames(t)
		for _, n := range webhookTools {
			if slices.Contains(names, n) != tc.want {
				t.Errorf("scopes %v: offers %s = %v, want %v", tc.scopes, n, !tc.want, tc.want)
			}
		}
	}
}

func TestReadOnlyKeepsOnlyNewReads(t *testing.T) {
	names := newHarness(t, Options{ReadOnly: true, Scopes: []string{"manage_inbox_settings", "manage_integrations", "manage_widgets", "chat:read"}}).toolNames(t)
	for _, n := range []string{"list_automations", "test_automation", "list_webhooks", "list_campaigns"} {
		if !slices.Contains(names, n) {
			t.Errorf("read-only lost %s", n)
		}
	}
	for _, n := range []string{"create_automation", "create_webhook", "send_test_webhook", "create_campaign", "delete_team"} {
		if slices.Contains(names, n) {
			t.Errorf("read-only offers %s", n)
		}
	}
}

func TestLocalFileToolsOnlyOnStdio(t *testing.T) {
	scopes := []string{"configure_ai", "manage_workspace_identity"}
	for _, local := range []bool{false, true} {
		names := newHarness(t, Options{Scopes: scopes, LocalFiles: local}).toolNames(t)
		for _, n := range []string{"upload_knowledge_file", "upload_logo"} {
			if slices.Contains(names, n) != local {
				t.Errorf("LocalFiles=%v: %s offered=%v", local, n, !local)
			}
		}
	}
}

func TestUploadKnowledgeFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "faq.md")
	if err := os.WriteFile(good, []byte("# Refunds\nWithin 30 days."), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Options{Scopes: []string{"configure_ai"}, LocalFiles: true})

	rejected := map[string]map[string]any{
		"unconfirmed":   {"path": good},
		"relative path": {"path": "faq.md", "confirm": true},
		"dotfile":       {"path": filepath.Join(dir, ".env"), "confirm": true},
		"bad extension": {"path": filepath.Join(dir, "id_rsa.key"), "confirm": true},
		"missing file":  {"path": filepath.Join(dir, "nope.pdf"), "confirm": true},
	}
	for name, args := range rejected {
		if res, _ := h.call(t, "upload_knowledge_file", args); !res.IsError {
			t.Errorf("%s: accepted", name)
		}
	}
	if calls := h.recorded(); len(calls) != 0 {
		t.Fatalf("rejected uploads sent requests: %v", calls)
	}

	if res, text := h.call(t, "upload_knowledge_file", map[string]any{"path": good, "title": "FAQ", "confirm": true}); res.IsError {
		t.Fatalf("upload failed: %s", text)
	}
	calls := h.recorded()
	if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/api/v1/rag/documents/upload" {
		t.Fatalf("calls = %v", calls)
	}
	for _, want := range []string{`name="file"; filename="faq.md"`, "Within 30 days.", `name="title"`, "FAQ"} {
		if !strings.Contains(calls[0].body, want) {
			t.Errorf("multipart body lacks %q", want)
		}
	}
}

func TestPathsAndBodies(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{
		"manage_inbox_settings", "manage_integrations", "manage_widgets", "view_insights", "chat:read", "configure_ai",
	}})
	cases := []struct {
		tool     string
		args     map[string]any
		method   string
		path     string
		bodyHas  []string
		bodyLack []string
	}{
		{"get_business_hours", map[string]any{}, "GET", "/api/v1/business-hours", nil, nil},
		{"get_business_hours", map[string]any{"inboxType": "widget", "inboxId": uuidA}, "GET", "/api/v1/inboxes/widget/" + uuidA + "/business-hours", nil, nil},
		{"set_business_hours", map[string]any{"enabled": true, "timeZone": "Africa/Cairo",
			"schedule": []any{map[string]any{"dayOfWeek": 0, "intervals": []any{map[string]any{"start": "09:00", "end": "17:00"}}}}},
			"PUT", "/api/v1/business-hours", []string{`"timeZone":"Africa/Cairo"`, `"dayOfWeek":0`}, []string{"inboxType"}},
		{"remove_inbox_member", map[string]any{"inboxType": "channel", "inboxId": uuidA, "userId": uuidB, "confirm": true},
			"DELETE", "/api/v1/inboxes/channel/" + uuidA + "/members/" + uuidB, nil, nil},
		{"add_team_member", map[string]any{"id": uuidA, "userId": uuidB}, "POST", "/api/v1/teams/" + uuidA + "/members", []string{`"userId":"` + uuidB + `"`}, nil},
		{"update_team", map[string]any{"id": uuidA, "name": "Billing", "allowAutoAssign": false}, "PUT", "/api/v1/teams/" + uuidA,
			[]string{`"allowAutoAssign":false`, `"name":"Billing"`}, []string{`"id"`}},
		{"update_campaign", map[string]any{"campaignId": uuidA, "name": "Pricing nudge", "message": "Questions?", "urlMatch": "contains",
			"urlValue": "/pricing", "delaySeconds": 10, "isActive": true}, "PUT", "/api/v1/campaigns/" + uuidA,
			[]string{`"urlMatch":"contains"`, `"isActive":true`}, []string{"campaignId"}},
		{"update_webhook", map[string]any{"webhookId": uuidA, "isActive": false}, "PUT", "/api/v1/webhooks/" + uuidA,
			[]string{`"isActive":false`}, []string{"eventTypes"}},
		{"list_webhook_deliveries", map[string]any{"webhookId": uuidA, "status": "failed"}, "GET",
			"/api/v1/webhooks/" + uuidA + "/deliveries?limit=25&offset=0&status=failed", nil, nil},
		{"retry_webhook_delivery", map[string]any{"deliveryId": uuidA}, "POST", "/api/v1/webhooks/deliveries/" + uuidA + "/retry", nil, nil},
		{"get_report_breakdown", map[string]any{"groupBy": "team", "from": "2026-11-01", "teamId": uuidA}, "GET",
			"/api/v1/reports/breakdown?from=2026-11-01&groupBy=team&teamId=" + uuidA, nil, nil},
		{"list_conversation_messages", map[string]any{"conversationId": uuidA, "limit": 500}, "GET",
			"/api/v1/agent/conversations/" + uuidA + "/messages?limit=100&page=1", nil, nil},
		{"create_automation", map[string]any{"name": "Tag refunds", "trigger": "message_created", "isActive": true,
			"conditions": map[string]any{"match": "all", "conditions": []any{map[string]any{"field": "message_text", "operator": "contains", "value": "refund"}}},
			"actions":    []any{map[string]any{"type": "add_label", "labelId": uuidA}}}, "POST", "/api/v1/automations",
			[]string{`"trigger":"message_created"`, `"labelId":"` + uuidA + `"`, `"isActive":true`}, []string{`"userId"`}},
		{"get_operation_fields", map[string]any{"operationId": "orders.refund"}, "GET", "/api/v1/admin/action-policies/operations/orders.refund/fields", nil, nil},
		{"connect_email", map[string]any{}, "POST", "/api/v1/channels/email/connect", nil, nil},
	}
	for _, c := range cases {
		before := len(h.recorded())
		if res, text := h.call(t, c.tool, c.args); res.IsError {
			t.Errorf("%s: %s", c.tool, text)
			continue
		}
		calls := h.recorded()[before:]
		if len(calls) != 1 {
			t.Errorf("%s: calls = %v", c.tool, calls)
			continue
		}
		got := calls[0]
		if got.method != c.method || got.path != c.path {
			t.Errorf("%s: %s %s, want %s %s", c.tool, got.method, got.path, c.method, c.path)
		}
		for _, s := range c.bodyHas {
			if !strings.Contains(got.body, s) {
				t.Errorf("%s: body %s lacks %s", c.tool, got.body, s)
			}
		}
		for _, s := range c.bodyLack {
			if strings.Contains(got.body, s) {
				t.Errorf("%s: body %s has %s", c.tool, got.body, s)
			}
		}
	}
}

func TestNewToolsRejectBadInputBeforeSending(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_inbox_settings", "manage_integrations", "manage_widgets", "configure_ai", "chat:read"}})
	cases := map[string]map[string]any{
		"delete_team":                {"id": uuidA},
		"create_webhook":             {"url": "https://example.com/hook", "eventTypes": []any{"conversation.created"}},
		"update_webhook":             {"webhookId": uuidA, "url": "https://evil.example"},
		"rotate_webhook_secret":      {"webhookId": uuidA},
		"delete_campaign":            {"campaignId": uuidA},
		"clear_inbox_business_hours": {"inboxType": "widget", "inboxId": uuidA},
		"remove_team_member":         {"id": uuidA, "userId": uuidB},
		"list_inbox_members":         {"inboxType": "../billing", "inboxId": uuidA},
		"get_business_hours":         {"inboxType": "widget"},
		"get_team":                   {"id": "../admin/users"},
		"create_campaign":            {"name": "x", "message": "y", "urlMatch": "any", "widgetIds": []any{"nope"}},
		"create_automation":          {"name": "x", "trigger": "message_created", "actions": []any{map[string]any{"type": "assign", "userId": "../x"}}},
		"get_operation_fields":       {"operationId": "../../billing"},
		"reorder_automations":        {"ids": []any{uuidA, "bad"}},
	}
	for tool, args := range cases {
		if res, _ := h.call(t, tool, args); !res.IsError {
			t.Errorf("%s accepted %v", tool, args)
		}
	}
	if calls := h.recorded(); len(calls) != 0 {
		t.Errorf("requests sent: %v", calls)
	}
}

func TestEmbeddedFieldsAppearInSchemas(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_widgets", "manage_inbox_settings"}})
	res, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"update_campaign":    {"campaignId", "name", "message", "urlMatch", "isActive"},
		"set_business_hours": {"inboxType", "enabled", "timeZone", "schedule"},
		"update_automation":  {"id", "trigger", "conditions", "actions"},
	}
	for _, tool := range res.Tools {
		props, ok := want[tool.Name]
		if !ok {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		_ = json.Unmarshal(raw, &schema)
		for _, p := range props {
			if _, ok := schema.Properties[p]; !ok {
				t.Errorf("%s schema lacks %s: %s", tool.Name, p, raw)
			}
		}
		delete(want, tool.Name)
	}
	if len(want) != 0 {
		t.Errorf("tools not found: %v", want)
	}
}
