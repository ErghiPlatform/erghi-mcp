package tools

import (
	"slices"
	"strings"
	"testing"
)

var (
	chatReadTools  = []string{"get_agent_inbox_options", "list_conversation_notes", "list_labels_on_conversation"}
	chatWriteTools = []string{"add_conversation_note", "add_label_to_conversation", "remove_label_from_conversation",
		"snooze_conversation", "unsnooze_conversation", "set_conversation_priority", "set_conversation_team",
		"assign_conversation", "unassign_conversation", "reopen_conversation", "bulk_update_conversations",
		"apply_macro", "run_reply_copilot"}
	contactReadTools  = []string{"list_contacts", "get_contact"}
	contactWriteTools = []string{"update_contact"}
)

func TestAgentToolsFollowTheirScope(t *testing.T) {
	groups := map[string][]string{
		"chat:read":      chatReadTools,
		"chat:write":     chatWriteTools,
		"contacts:read":  contactReadTools,
		"contacts:write": contactWriteTools,
	}
	for scope := range groups {
		names := newHarness(t, Options{Scopes: []string{scope}}).toolNames(t)
		for other, tools := range groups {
			for _, n := range tools {
				if got := slices.Contains(names, n); got != (other == scope) {
					t.Errorf("scope %s: %s offered=%v", scope, n, got)
				}
			}
		}
	}

	// Setup scopes never unlock agent work.
	names := newHarness(t, Options{Scopes: []string{"manage_inbox_settings", "configure_ai"}}).toolNames(t)
	for _, tools := range groups {
		for _, n := range tools {
			if slices.Contains(names, n) {
				t.Errorf("setup scopes offer %s", n)
			}
		}
	}
}

func TestAgentToolsReadOnlyKeepsOnlyReads(t *testing.T) {
	names := newHarness(t, Options{ReadOnly: true, Scopes: []string{"chat:read", "chat:write", "contacts:read", "contacts:write"}}).toolNames(t)
	for _, n := range append(slices.Clone(chatReadTools), contactReadTools...) {
		if !slices.Contains(names, n) {
			t.Errorf("read-only lost %s", n)
		}
	}
	for _, n := range append(slices.Clone(chatWriteTools), contactWriteTools...) {
		if slices.Contains(names, n) {
			t.Errorf("read-only offers %s", n)
		}
	}
}

func TestAgentToolPathsAndBodies(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"chat:read", "chat:write", "contacts:read", "contacts:write"}})
	c := "/api/v1/agent/conversations/" + uuidA
	cases := []struct {
		tool     string
		args     map[string]any
		method   string
		path     string
		bodyHas  []string
		bodyLack []string
	}{
		{"get_agent_inbox_options", map[string]any{}, "GET", "/api/v1/agent/inbox", nil, nil},
		{"list_conversation_notes", map[string]any{"conversationId": uuidA}, "GET", c + "/notes", nil, nil},
		{"list_labels_on_conversation", map[string]any{"conversationId": uuidA}, "GET", c + "/labels", nil, nil},
		{"add_conversation_note", map[string]any{"conversationId": uuidA, "content": "VIP"}, "POST", c + "/notes",
			[]string{`"content":"VIP"`}, []string{"mentionedUserIds"}},
		{"add_conversation_note", map[string]any{"conversationId": uuidA, "content": "x", "mentionedUserIds": []any{uuidB}}, "POST", c + "/notes",
			[]string{`"mentionedUserIds":["` + uuidB + `"]`}, nil},
		{"add_label_to_conversation", map[string]any{"conversationId": uuidA, "labelId": uuidB}, "POST", c + "/labels/" + uuidB, nil, nil},
		{"remove_label_from_conversation", map[string]any{"conversationId": uuidA, "labelId": uuidB}, "DELETE", c + "/labels/" + uuidB, nil, nil},
		{"snooze_conversation", map[string]any{"conversationId": uuidA, "snoozeMinutes": 60}, "POST", c + "/snooze",
			[]string{`"snoozeMinutes":60`, `"untilNextReply":false`}, nil},
		{"unsnooze_conversation", map[string]any{"conversationId": uuidA}, "DELETE", c + "/snooze", nil, nil},
		{"set_conversation_priority", map[string]any{"conversationId": uuidA, "priority": "urgent"}, "PUT", c + "/priority",
			[]string{`"priority":"urgent"`}, nil},
		{"set_conversation_team", map[string]any{"conversationId": uuidA, "teamId": uuidB}, "PUT", c + "/team",
			[]string{`"teamId":"` + uuidB + `"`}, nil},
		{"set_conversation_team", map[string]any{"conversationId": uuidA, "teamId": nil}, "PUT", c + "/team",
			[]string{`"teamId":null`}, nil},
		{"assign_conversation", map[string]any{"conversationId": uuidA, "userId": uuidB}, "POST", c + "/assign",
			[]string{`"agentId":"` + uuidB + `"`}, nil},
		{"unassign_conversation", map[string]any{"conversationId": uuidA}, "POST", c + "/unassign", nil, nil},
		{"reopen_conversation", map[string]any{"conversationId": uuidA}, "POST", c + "/reopen", nil, nil},
		{"bulk_update_conversations", map[string]any{"conversationIds": []any{uuidA, uuidB}, "action": "set_priority",
			"payload": map[string]any{"priority": "low"}, "confirm": true}, "POST", "/api/v1/agent/conversations/bulk",
			[]string{`"conversationIds":["` + uuidA + `","` + uuidB + `"]`, `"action":"set_priority"`, `"priority":"low"`}, []string{"confirm"}},
		{"apply_macro", map[string]any{"conversationId": uuidA, "macroId": uuidB, "confirm": true}, "POST", c + "/macros/" + uuidB + "/apply", nil, nil},
		{"run_reply_copilot", map[string]any{"conversationId": uuidA, "action": "rewrite", "text": "hi", "tone": "formal"}, "POST", c + "/copilot",
			[]string{`"action":"rewrite"`, `"tone":"formal"`}, []string{"targetLanguage", "messageId"}},
		{"list_contacts", map[string]any{"search": "mona", "pageSize": 500}, "GET", "/api/v1/agent/contacts?page=1&pageSize=100&search=mona", nil, nil},
		{"get_contact", map[string]any{"contactId": uuidA}, "GET", "/api/v1/agent/contacts/" + uuidA, nil, nil},
		{"update_contact", map[string]any{"contactId": uuidA, "name": "Mona Ali"}, "PATCH", "/api/v1/agent/contacts/" + uuidA,
			[]string{`"name":"Mona Ali"`, `"email":null`, `"phone":null`}, nil},
	}
	for _, tc := range cases {
		before := len(h.recorded())
		if res, text := h.call(t, tc.tool, tc.args); res.IsError {
			t.Errorf("%s: %s", tc.tool, text)
			continue
		}
		calls := h.recorded()[before:]
		if len(calls) != 1 {
			t.Errorf("%s: %d calls", tc.tool, len(calls))
			continue
		}
		got := calls[0]
		if got.method != tc.method || got.path != tc.path {
			t.Errorf("%s: %s %s, want %s %s", tc.tool, got.method, got.path, tc.method, tc.path)
		}
		for _, s := range tc.bodyHas {
			if !strings.Contains(got.body, s) {
				t.Errorf("%s: body %s lacks %s", tc.tool, got.body, s)
			}
		}
		for _, s := range tc.bodyLack {
			if strings.Contains(got.body, s) {
				t.Errorf("%s: body %s has %s", tc.tool, got.body, s)
			}
		}
	}
}

func TestAgentToolsRejectBadInputBeforeSending(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"chat:read", "chat:write", "contacts:read", "contacts:write"}})
	tooMany := make([]any, maxBulk+1)
	for i := range tooMany {
		tooMany[i] = uuidA
	}
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"list_conversation_notes", map[string]any{"conversationId": "../../contacts"}},
		{"add_conversation_note", map[string]any{"conversationId": uuidA, "content": "x", "mentionedUserIds": []any{"me"}}},
		{"add_label_to_conversation", map[string]any{"conversationId": uuidA, "labelId": "../../bulk"}},
		{"set_conversation_priority", map[string]any{"conversationId": uuidA, "priority": "critical"}},
		{"set_conversation_team", map[string]any{"conversationId": uuidA, "teamId": "x"}},
		{"assign_conversation", map[string]any{"conversationId": uuidA, "userId": "admin"}},
		// Bulk and macros act on many things or can message the customer, so they need confirm.
		{"bulk_update_conversations", map[string]any{"conversationIds": []any{uuidA}, "action": "resolve"}},
		{"bulk_update_conversations", map[string]any{"conversationIds": []any{uuidA}, "action": "delete", "confirm": true}},
		{"bulk_update_conversations", map[string]any{"conversationIds": []any{}, "action": "resolve", "confirm": true}},
		{"bulk_update_conversations", map[string]any{"conversationIds": tooMany, "action": "resolve", "confirm": true}},
		{"bulk_update_conversations", map[string]any{"conversationIds": []any{"x"}, "action": "resolve", "confirm": true}},
		{"bulk_update_conversations", map[string]any{"conversationIds": []any{uuidA}, "action": "assign",
			"payload": map[string]any{"userId": "../x"}, "confirm": true}},
		{"apply_macro", map[string]any{"conversationId": uuidA, "macroId": uuidB}},
		{"apply_macro", map[string]any{"conversationId": uuidA, "macroId": "x", "confirm": true}},
		{"run_reply_copilot", map[string]any{"conversationId": uuidA, "action": "send"}},
		{"run_reply_copilot", map[string]any{"conversationId": uuidA, "action": "rewrite", "text": "x", "tone": "angry"}},
		{"run_reply_copilot", map[string]any{"conversationId": uuidA, "action": "translate", "messageId": "x", "targetLanguage": "ar"}},
		{"get_contact", map[string]any{"contactId": "../../conversations"}},
		{"update_contact", map[string]any{"contactId": uuidA}},
	}
	for _, tc := range cases {
		if res, _ := h.call(t, tc.tool, tc.args); !res.IsError {
			t.Errorf("%s accepted %v", tc.tool, tc.args)
		}
	}
	if calls := h.recorded(); len(calls) != 0 {
		t.Errorf("requests sent: %v", calls)
	}
}
