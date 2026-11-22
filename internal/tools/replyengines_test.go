package tools

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

var replyEngineAdminTools = []string{
	"list_reply_engines", "get_reply_engine", "create_reply_engine", "update_reply_engine", "delete_reply_engine",
	"set_reply_engine_active", "rotate_reply_engine_secret", "test_reply_engine", "get_reply_engine_health",
	"list_reply_engine_turns", "get_widget_reply_engine", "set_widget_reply_engine",
}

func TestReplyEngineToolsFollowTheirScope(t *testing.T) {
	cases := []struct {
		scopes []string
		want   []string
	}{
		{[]string{"configure_ai", "agent:runtime"}, replyEngineAdminTools},
		// Without agent:runtime the API refuses a key that creates, changes, re-keys or binds an engine.
		{[]string{"configure_ai"}, []string{
			"list_reply_engines", "get_reply_engine", "delete_reply_engine", "set_reply_engine_active",
			"test_reply_engine", "get_reply_engine_health", "list_reply_engine_turns", "get_widget_reply_engine",
		}},
		{[]string{"manage_widgets"}, []string{"list_reply_engines", "get_widget_reply_engine"}},
		{[]string{"manage_integrations", "chat:read", "agent:runtime"}, nil},
	}
	for _, tc := range cases {
		names := newHarness(t, Options{Scopes: tc.scopes}).toolNames(t)
		for _, n := range replyEngineAdminTools {
			if got, want := slices.Contains(names, n), slices.Contains(tc.want, n); got != want {
				t.Errorf("scopes %v: %s offered=%v, want %v", tc.scopes, n, got, want)
			}
		}
	}
	for _, n := range newHarness(t, Options{Scopes: []string{"configure_ai", "agent:runtime"}}).toolNames(t) {
		if strings.Contains(n, "send_bot_reply") || strings.Contains(n, "decline_turn") || strings.Contains(n, "handoff_to_human") {
			t.Errorf("runtime tool %s offered", n)
		}
	}
	ro := newHarness(t, Options{ReadOnly: true, Scopes: []string{"configure_ai"}}).toolNames(t)
	for _, n := range []string{"list_reply_engines", "get_reply_engine", "get_reply_engine_health", "list_reply_engine_turns", "get_widget_reply_engine"} {
		if !slices.Contains(ro, n) {
			t.Errorf("read-only lost %s", n)
		}
	}
	for _, n := range []string{"create_reply_engine", "update_reply_engine", "delete_reply_engine", "set_reply_engine_active", "rotate_reply_engine_secret", "test_reply_engine", "set_widget_reply_engine"} {
		if slices.Contains(ro, n) {
			t.Errorf("read-only offers %s", n)
		}
	}
}

func TestReplyEnginePathsAndBodies(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "agent:runtime"}})
	cases := []struct {
		tool     string
		args     map[string]any
		method   string
		path     string
		bodyHas  []string
		bodyLack []string
	}{
		{"list_reply_engines", map[string]any{}, "GET", "/api/v1/reply-engines", nil, nil},
		{"list_reply_engines", map[string]any{"namesOnly": true}, "GET", "/api/v1/reply-engines?view=names", nil, nil},
		{"get_reply_engine", map[string]any{"engineId": uuidA}, "GET", "/api/v1/reply-engines/" + uuidA, nil, nil},
		{"create_reply_engine", map[string]any{"name": "Support bot", "mode": "Webhook", "url": "https://agent.example.com/turns", "confirm": true},
			"POST", "/api/v1/reply-engines", []string{`"name":"Support bot"`, `"mode":"Webhook"`, `"url":"https://agent.example.com/turns"`}, []string{"confirm", "apiKey"}},
		{"update_reply_engine", map[string]any{"engineId": uuidA, "timeoutSeconds": 20, "fallback": "Human", "confirm": true},
			"PATCH", "/api/v1/reply-engines/" + uuidA, []string{`"timeoutSeconds":20`, `"fallback":"Human"`}, []string{"confirm", "engineId", `"mode"`, `"apiKey"`}},
		{"delete_reply_engine", map[string]any{"engineId": uuidA, "confirm": true}, "DELETE", "/api/v1/reply-engines/" + uuidA, nil, nil},
		{"set_reply_engine_active", map[string]any{"engineId": uuidA, "active": true, "confirm": true}, "POST", "/api/v1/reply-engines/" + uuidA + "/enable", nil, nil},
		{"set_reply_engine_active", map[string]any{"engineId": uuidA, "active": false, "confirm": true}, "POST", "/api/v1/reply-engines/" + uuidA + "/disable", nil, nil},
		{"rotate_reply_engine_secret", map[string]any{"engineId": uuidA, "confirm": true}, "POST", "/api/v1/reply-engines/" + uuidA + "/rotate-secret", nil, nil},
		{"test_reply_engine", map[string]any{"engineId": uuidA}, "POST", "/api/v1/reply-engines/" + uuidA + "/test", nil, nil},
		{"get_reply_engine_health", map[string]any{"engineId": uuidA}, "GET", "/api/v1/reply-engines/" + uuidA + "/health", nil, nil},
		{"list_reply_engine_turns", map[string]any{"engineId": uuidA, "cursor": "abc", "limit": 500, "includeTests": true}, "GET",
			"/api/v1/reply-engines/" + uuidA + "/turns?cursor=abc&includeTests=true&limit=100", nil, nil},
		{"get_widget_reply_engine", map[string]any{"widgetId": uuidB}, "GET", "/api/v1/widgets/" + uuidB + "/reply-engine", nil, nil},
		{"set_widget_reply_engine", map[string]any{"widgetId": uuidB, "default": uuidA, "channels": map[string]any{"telegram": "erghi", "whatsapp": nil}, "confirm": true},
			"PUT", "/api/v1/widgets/" + uuidB + "/reply-engine", []string{`"default":"` + uuidA + `"`, `"telegram":"erghi"`, `"whatsapp":null`}, []string{"confirm", "widgetId"}},
		{"set_widget_reply_engine", map[string]any{"widgetId": uuidB, "confirm": true}, "PUT", "/api/v1/widgets/" + uuidB + "/reply-engine", []string{`"default":null`}, nil},
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

func TestReplyEngineNamesOnlyKeyAlwaysAsksForNames(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_widgets"}})
	if res, text := h.call(t, "list_reply_engines", map[string]any{}); res.IsError {
		t.Fatal(text)
	}
	if calls := h.recorded(); len(calls) != 1 || calls[0].path != "/api/v1/reply-engines?view=names" {
		t.Errorf("calls = %v", calls)
	}
}

func TestReplyEngineWritesNeedConfirmAndValidIDs(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "agent:runtime"}})
	cases := map[string]map[string]any{
		"create_reply_engine":        {"name": "x", "mode": "Webhook", "url": "https://a.example"},
		"update_reply_engine":        {"engineId": uuidA, "name": "y"},
		"delete_reply_engine":        {"engineId": uuidA},
		"set_reply_engine_active":    {"engineId": uuidA, "active": false},
		"rotate_reply_engine_secret": {"engineId": uuidA},
		"set_widget_reply_engine":    {"widgetId": uuidB, "default": uuidA},
		"get_reply_engine":           {"engineId": "../billing"},
		"list_reply_engine_turns":    {"engineId": "nope"},
		"test_reply_engine":          {"engineId": "../x"},
		"get_widget_reply_engine":    {"widgetId": "../x"},
	}
	badRefs := map[string]map[string]any{
		"bad default":   {"widgetId": uuidB, "default": "../x", "confirm": true},
		"bad channel":   {"widgetId": uuidB, "channels": map[string]any{"telegram": "something"}, "confirm": true},
		"empty channel": {"widgetId": uuidB, "channels": map[string]any{" ": "erghi"}, "confirm": true},
	}
	for tool, args := range cases {
		if res, _ := h.call(t, tool, args); !res.IsError {
			t.Errorf("%s accepted %v", tool, args)
		}
	}
	for name, args := range badRefs {
		if res, _ := h.call(t, "set_widget_reply_engine", args); !res.IsError {
			t.Errorf("%s accepted", name)
		}
	}
	if calls := h.recorded(); len(calls) != 0 {
		t.Errorf("requests sent: %v", calls)
	}
}

func TestReplyEngineSecretNotice(t *testing.T) {
	out, err := withSecretNotice([]byte(`{"id":"a","secret":"whsec_abc"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || !strings.Contains(m["storeThisNow"].(string), "only once") {
		t.Errorf("no notice: %v", out)
	}
	for _, body := range []string{`{"id":"a","secret":null}`, `{"id":"a"}`} {
		out, _ := withSecretNotice([]byte(body), nil)
		if _, wrapped := out.(map[string]any); wrapped {
			t.Errorf("%s wrapped without a secret", body)
		}
	}
}

func TestReplyEngineSchemasIncludeSettings(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"configure_ai", "agent:runtime"}})
	res, err := h.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "create_reply_engine" {
			continue
		}
		raw := fmt.Sprint(tool.InputSchema)
		for _, p := range []string{"name", "mode", "url", "baseUrl", "apiKey", "turnsQueueUrl", "confirm"} {
			if !strings.Contains(raw, p) {
				t.Errorf("create_reply_engine schema lacks %s", p)
			}
		}
		return
	}
	t.Error("create_reply_engine not found")
}
