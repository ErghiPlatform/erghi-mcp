package tools

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestWorkforceAndSocialToolsFollowTheirScope(t *testing.T) {
	cases := map[string][]string{
		"manage_workforce": {"set_agent_status", "sign_out_agent", "create_away_reason", "update_away_reason", "archive_away_reason",
			"create_routing_skill", "update_routing_skill", "archive_routing_skill", "get_agent_routing_profile",
			"update_agent_routing_profile", "update_workforce_settings", "get_assignment_settings", "update_assignment_settings",
			"list_away_reasons", "list_routing_skills", "get_workforce_settings", "get_workforce_live"},
		"moderate_social": {"list_social_posts", "list_social_comments", "hide_social_comment", "unhide_social_comment", "mark_social_comment_done",
			"reply_to_social_comment", "private_reply_to_social_comment", "ignore_social_commenter"},
		"view_insights": {"get_agent_status_report", "get_agent_status_timeline", "get_agent_performance_report", "get_social_report"},
		"configure_ai":  {"list_social_connections", "get_social_connection_settings", "update_social_connection_settings", "get_social_disconnect_preview"},
	}
	own := map[string][]string{} // tools no other scope in the table shares
	for scope, tools := range cases {
		for _, n := range tools {
			shared := false
			for other, list := range cases {
				if other != scope && slices.Contains(list, n) {
					shared = true
				}
			}
			if !shared {
				own[scope] = append(own[scope], n)
			}
		}
	}
	for scope, want := range cases {
		names := newHarness(t, Options{Scopes: []string{scope}}).toolNames(t)
		for _, n := range want {
			if !slices.Contains(names, n) {
				t.Errorf("scope %s: missing %s", scope, n)
			}
		}
		for other, list := range own {
			if other == scope {
				continue
			}
			for _, n := range list {
				if slices.Contains(names, n) && !slices.Contains(want, n) {
					t.Errorf("scope %s: offers %s, which belongs to %s", scope, n, other)
				}
			}
		}
	}
	// Assignment settings stay with manage_inbox_settings too.
	names := newHarness(t, Options{Scopes: []string{"manage_inbox_settings"}}).toolNames(t)
	if !slices.Contains(names, "update_assignment_settings") || slices.Contains(names, "set_agent_status") {
		t.Errorf("inbox scope tools = %v", names)
	}
}

func TestWorkforceAndSocialPaths(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_workforce", "moderate_social", "view_insights", "configure_ai"}})
	cases := []struct {
		tool     string
		args     map[string]any
		method   string
		path     string
		bodyHas  []string
		bodyLack []string
	}{
		{"set_agent_status", map[string]any{"userId": uuidA, "state": "away", "reasonId": uuidB, "returnChats": true, "confirm": true},
			"PUT", "/api/v1/workforce/agents/" + uuidA + "/status", []string{`"state":"away"`, `"reasonId":"` + uuidB + `"`, `"returnChats":true`}, []string{"confirm", "userId"}},
		{"sign_out_agent", map[string]any{"userId": uuidA, "confirm": true}, "POST", "/api/v1/workforce/agents/" + uuidA + "/sign-out", nil, nil},
		{"create_away_reason", map[string]any{"name": "Lunch", "maxMinutes": 45, "reassignChats": false}, "POST", "/api/v1/workforce/away-reasons",
			[]string{`"name":"Lunch"`, `"reassignChats":false`, `"maxMinutes":45`}, nil},
		{"update_away_reason", map[string]any{"id": uuidA, "name": "Lunch", "reassignChats": true, "archived": false}, "PUT", "/api/v1/workforce/away-reasons/" + uuidA,
			[]string{`"reassignChats":true`, `"archived":false`}, []string{`"id"`}},
		{"archive_away_reason", map[string]any{"id": uuidA, "confirm": true}, "DELETE", "/api/v1/workforce/away-reasons/" + uuidA, nil, nil},
		{"create_routing_skill", map[string]any{"name": "Billing", "description": "Invoices and refunds"}, "POST", "/api/v1/workforce/skills", []string{`"description":"Invoices and refunds"`}, nil},
		{"update_routing_skill", map[string]any{"id": uuidA, "name": "Billing", "description": "x", "archived": true}, "PUT", "/api/v1/workforce/skills/" + uuidA, []string{`"archived":true`}, []string{`"id"`}},
		{"archive_routing_skill", map[string]any{"id": uuidA, "confirm": true}, "DELETE", "/api/v1/workforce/skills/" + uuidA, nil, nil},
		{"get_agent_routing_profile", map[string]any{"userId": uuidA}, "GET", "/api/v1/workforce/agents/" + uuidA + "/profile", nil, nil},
		{"update_agent_routing_profile", map[string]any{"userId": uuidA, "maxConcurrentChats": 0, "skills": []any{map[string]any{"skillId": uuidB, "level": 2}}},
			"PUT", "/api/v1/workforce/agents/" + uuidA + "/profile", []string{`"maxConcurrentChats":0`, `"languages":[]`, `"skillId":"` + uuidB + `"`}, []string{"userId"}},
		{"update_agent_routing_profile", map[string]any{"userId": uuidA}, "PUT", "/api/v1/workforce/agents/" + uuidA + "/profile", []string{`"languages":[]`}, []string{"skills", "maxConcurrentChats"}},
		{"update_workforce_settings", map[string]any{"statusOnSignIn": "ready", "requireAwayReason": true, "idleAwayMinutes": 10,
			"routing": map[string]any{"offerMode": "ring", "offerTimeoutSeconds": 20}}, "PUT", "/api/v1/workforce/settings",
			[]string{`"statusOnSignIn":"ready"`, `"requireAwayReason":true`, `"idleAwayMinutes":10`, `"offerMode":"ring"`}, []string{"signOutAfterHoursMinutes"}},
		// Partial update: only what was sent goes out, and 0 (turn off) is sent, not dropped.
		{"update_workforce_settings", map[string]any{"idleAwayMinutes": 0}, "PUT", "/api/v1/workforce/settings",
			[]string{`"idleAwayMinutes":0`}, []string{"statusOnSignIn", "requireAwayReason", "signOutAfterHoursMinutes", "autoAwayAfterMissedOffers", "routing"}},
		{"update_workforce_settings", map[string]any{"requireAwayReason": false}, "PUT", "/api/v1/workforce/settings",
			[]string{`"requireAwayReason":false`}, []string{"statusOnSignIn", "idleAwayMinutes"}},
		{"get_workforce_settings", map[string]any{}, "GET", "/api/v1/workforce/settings", nil, nil},
		{"get_workforce_live", map[string]any{}, "GET", "/api/v1/workforce/live", nil, nil},
		{"list_away_reasons", map[string]any{}, "GET", "/api/v1/workforce/away-reasons", nil, nil},
		{"list_routing_skills", map[string]any{"includeArchived": true}, "GET", "/api/v1/workforce/skills?includeArchived=true", nil, nil},
		{"get_agent_status_report", map[string]any{"from": "2026-11-01", "groupBy": "team"}, "GET", "/api/v1/reports/agent-status?from=2026-11-01&groupBy=team", nil, nil},
		{"get_agent_status_timeline", map[string]any{"agentId": uuidA, "timeZone": "Africa/Cairo"}, "GET",
			"/api/v1/reports/agent-status/timeline?agentId=" + uuidA + "&timeZone=Africa%2FCairo", nil, nil},
		{"get_agent_performance_report", map[string]any{}, "GET", "/api/v1/reports/agent-performance", nil, nil},
		{"get_assignment_settings", map[string]any{}, "GET", "/api/v1/assignment-settings", nil, nil},

		{"list_social_connections", map[string]any{}, "GET", "/api/v1/channels/meta/connections", nil, nil},
		{"get_social_connection_settings", map[string]any{"connectionId": uuidA}, "GET", "/api/v1/channels/meta/connections/" + uuidA + "/settings", nil, nil},
		{"update_social_connection_settings", map[string]any{"connectionId": uuidA, "autoHideSpam": false, "allowedLinkDomains": []any{}, "commentsEnabled": false},
			"PUT", "/api/v1/channels/meta/connections/" + uuidA + "/settings", []string{`"autoHideSpam":false`, `"allowedLinkDomains":[]`, `"commentsEnabled":false`}, []string{"connectionId", "commentReplyMode"}},
		{"get_social_disconnect_preview", map[string]any{"connectionId": uuidA}, "GET", "/api/v1/channels/meta/connections/" + uuidA + "/disconnect-preview", nil, nil},
		{"list_social_comments", map[string]any{"connectionId": uuidA, "needsAttention": true, "category": "complaint", "pageSize": 500}, "GET",
			"/api/v1/social/comments?category=complaint&connectionId=" + uuidA + "&needsAttention=true&page=1&pageSize=100", nil, nil},
		{"list_social_posts", map[string]any{"page": 2}, "GET", "/api/v1/social/posts?page=2", nil, nil},
		{"hide_social_comment", map[string]any{"commentId": uuidA}, "POST", "/api/v1/social/comments/" + uuidA + "/hide", nil, nil},
		{"unhide_social_comment", map[string]any{"commentId": uuidA}, "POST", "/api/v1/social/comments/" + uuidA + "/unhide", nil, nil},
		{"mark_social_comment_done", map[string]any{"commentId": uuidA}, "POST", "/api/v1/social/comments/" + uuidA + "/done", nil, nil},
		{"reply_to_social_comment", map[string]any{"commentId": uuidA, "text": "Thanks!", "confirm": true}, "POST", "/api/v1/social/comments/" + uuidA + "/reply",
			[]string{`{"text":"Thanks!"}`}, nil},
		{"private_reply_to_social_comment", map[string]any{"commentId": uuidA, "text": "Hi", "confirm": true}, "POST", "/api/v1/social/comments/" + uuidA + "/private-reply",
			[]string{`{"text":"Hi"}`}, nil},
		{"ignore_social_commenter", map[string]any{"commentId": uuidA, "confirm": true}, "POST", "/api/v1/social/comments/" + uuidA + "/ignore-contact", nil, nil},
		{"get_social_report", map[string]any{"from": "2026-11-01"}, "GET", "/api/v1/reports/social?from=2026-11-01", nil, nil},
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

func TestWorkforceAndSocialRejectBadInputBeforeSending(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_workforce", "moderate_social", "view_insights", "configure_ai"}})
	cases := map[string]map[string]any{
		"set_agent_status":                   {"userId": uuidA, "state": "away"},
		"sign_out_agent":                     {"userId": uuidA},
		"archive_away_reason":                {"id": uuidA},
		"archive_routing_skill":              {"id": uuidA},
		"reply_to_social_comment":            {"commentId": uuidA, "text": "hi"},
		"private_reply_to_social_comment":    {"commentId": uuidA, "text": "hi"},
		"ignore_social_commenter":            {"commentId": uuidA},
		"get_agent_routing_profile":          {"userId": "../x"},
		"set_agent_status ":                  {"userId": uuidA, "state": "away", "reasonId": "nope", "confirm": true},
		"update_agent_routing_profile":       {"userId": uuidA, "skills": []any{map[string]any{"skillId": "bad", "level": 1}}},
		"get_agent_status_timeline":          {"agentId": "../x"},
		"hide_social_comment":                {"commentId": "../x"},
		"list_social_comments":               {"postId": "nope"},
		"get_social_connection_settings":     {"connectionId": "../x"},
		"update_social_connection_settings ": {"connectionId": "nope"},
	}
	for tool, args := range cases {
		if res, _ := h.call(t, strings.TrimSpace(tool), args); !res.IsError {
			t.Errorf("%s accepted %v", tool, args)
		}
	}
	if calls := h.recorded(); len(calls) != 0 {
		t.Errorf("requests sent: %v", calls)
	}
}

func TestOlderServersAndReadableErrors(t *testing.T) {
	h := newHarness(t, Options{Scopes: []string{"manage_workforce", "moderate_social", "configure_ai"}})
	h.override = func(w http.ResponseWriter, r *http.Request) bool {
		switch {
		case strings.Contains(r.URL.Path, "/workforce/"):
			if strings.HasSuffix(r.URL.Path, "/profile") { // the API's own "agent not found"
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Agent not found.","code":"not_found"}`))
				return true
			}
			w.WriteHeader(http.StatusNotFound) // unknown route: empty body
			return true
		case strings.HasSuffix(r.URL.Path, "/settings") && r.Method == http.MethodPut:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"That is a link shortener.","message":"shared_host_not_allowed","code":"shared_host_not_allowed"}`))
			return true
		case strings.HasSuffix(r.URL.Path, "/hide"):
			w.WriteHeader(http.StatusNotFound)
			return true
		case strings.HasSuffix(r.URL.Path, "/reply"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"Reconnect this account to moderate its comments.","code":"needs_reauth"}`))
			return true
		}
		return false
	}
	check := func(tool string, args map[string]any, want ...string) {
		t.Helper()
		res, text := h.call(t, tool, args)
		if !res.IsError {
			t.Errorf("%s: expected an error", tool)
		}
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: %q lacks %q", tool, text, w)
			}
		}
	}
	check("create_away_reason", map[string]any{"name": "x", "reassignChats": false}, "doesn't support workforce management yet")
	check("get_agent_routing_profile", map[string]any{"userId": uuidA}, "Agent not found", "not_found")
	check("update_social_connection_settings", map[string]any{"connectionId": uuidA, "allowedLinkDomains": []any{"bit.ly"}},
		"shared_host_not_allowed", "shared host")
	check("hide_social_comment", map[string]any{"commentId": uuidA}, "comment not found")
	check("reply_to_social_comment", map[string]any{"commentId": uuidA, "text": "hi", "confirm": true}, "needs_reauth", "reconnect")
}
