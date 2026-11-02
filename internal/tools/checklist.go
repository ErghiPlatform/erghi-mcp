package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// checklistStep mirrors one step of the "Get your assistant live" checklist
// on the admin dashboard, plus the persona step the dashboard leaves out.
type checklistStep struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"` // done, todo, unknown
	Detail string `json:"detail"`
	// Next says how to finish the step: tool names, or an admin-app link
	// when keys can't do it.
	Next string `json:"next,omitempty"`
}

type checklist struct {
	Done    int             `json:"done"`
	Total   int             `json:"total"`
	Steps   []checklistStep `json:"steps"`
	AppURL  string          `json:"adminAppUrl"`
	Summary string          `json:"summary"`
}

// RegisterChecklist adds get_setup_checklist. It runs after RegisterRemote
// because the conversation step counts through the hosted list_conversations
// tool (REST conversation endpoints are closed to API keys).
func RegisterChecklist(server *mcp.Server, api *erghi.Client, opts Options, rem *Remote) {
	r := &registry{server: server, api: api, opts: opts}
	add(r, read, nil, &mcp.Tool{
		Name: "get_setup_checklist",
		Description: "The workspace's setup progress, matching the 'Get your assistant live' checklist on the Erghi dashboard " +
			"(widget, knowledge, teammate, first conversation) plus the assistant's persona. Call it at the start and end of a setup.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return buildChecklist(ctx, r, rem), nil
	})
}

func buildChecklist(ctx context.Context, r *registry, rem *Remote) checklist {
	app := r.opts.AppURL
	steps := []checklistStep{
		personaStep(ctx, r, app),
		countStep(ctx, r, app, "widget", "Create a widget", scopeWidgets, "/api/widgets",
			"create_widget, then get_widget_embed_code", "/widgets"),
		countStep(ctx, r, app, "knowledge", "Add knowledge", scopeConfigureAI, "/api/v1/rag/documents",
			"crawl_website, add_knowledge_document or add_faq", "/rag-documents"),
		{
			Key: "teammate", Title: "Invite a teammate", Status: "unknown",
			Detail: "API keys can't see or invite team members, by design.",
			Next:   "Invite someone who can take over when a visitor asks for a human: " + app + "/users",
		},
		conversationStep(ctx, rem, r.opts.Scopes, app),
	}
	c := checklist{Total: len(steps), Steps: steps, AppURL: app}
	for _, s := range steps {
		if s.Status == "done" {
			c.Done++
		}
	}
	c.Summary = "Setup checklist: " + itoa(c.Done) + " of " + itoa(c.Total) + " steps done."
	return c
}

func personaStep(ctx context.Context, r *registry, app string) checklistStep {
	s := checklistStep{Key: "assistant", Title: "Give the assistant a name and instructions"}
	if !r.hasAny(needBranding) {
		return unknownScope(s, scopeConfigureAI, app+"/workspace")
	}
	raw, err := r.api.Do(ctx, http.MethodGet, "/api/v1/branding", nil, nil)
	if err != nil {
		return failed(s, err)
	}
	var b struct {
		AiAssistantName *string `json:"aiAssistantName"`
		AiSystemPrompt  *string `json:"aiSystemPrompt"`
		CompanyName     *string `json:"companyName"`
	}
	_ = json.Unmarshal(raw, &b)
	set := func(p *string) bool { return p != nil && *p != "" }
	if set(b.AiAssistantName) && set(b.AiSystemPrompt) {
		s.Status, s.Detail = "done", "Assistant is named "+*b.AiAssistantName+" and has instructions."
		return s
	}
	s.Status = "todo"
	s.Detail = "No assistant name or instructions yet, so replies use generic defaults."
	s.Next = "update_branding (aiAssistantName, aiSystemPrompt, aiTone, companyName)"
	return s
}

func countStep(ctx context.Context, r *registry, app, key, title, scope, path, next, appPath string) checklistStep {
	s := checklistStep{Key: key, Title: title}
	if !r.hasAny([]string{scope}) {
		return unknownScope(s, scope, app+appPath)
	}
	raw, err := r.api.Do(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return failed(s, err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return failed(s, err)
	}
	if len(items) > 0 {
		s.Status, s.Detail = "done", itoa(len(items))+" so far."
		return s
	}
	s.Status, s.Detail, s.Next = "todo", "None yet.", next
	return s
}

func conversationStep(ctx context.Context, rem *Remote, scopes []string, app string) checklistStep {
	s := checklistStep{Key: "conversation", Title: "Get your first conversation"}
	if !slices.Contains(scopes, "chat:read") || rem == nil {
		return unknownScope(s, "chat:read", app+"/conversations")
	}
	res := rem.Call(ctx, "list_conversations", map[string]any{})
	if res.IsError {
		return failed(s, errors.New(textOf(res)))
	}
	var page struct {
		Total int `json:"Total"`
	}
	if err := json.Unmarshal([]byte(textOf(res)), &page); err != nil {
		return failed(s, err)
	}
	if page.Total > 0 {
		s.Status, s.Detail = "done", itoa(page.Total)+" conversations so far."
		return s
	}
	s.Status = "todo"
	s.Detail = "No conversations yet."
	s.Next = "Paste the embed code on the site, open it and send a message through the widget, then check again."
	return s
}

func unknownScope(s checklistStep, scope, link string) checklistStep {
	s.Status = "unknown"
	s.Detail = "This API key has no " + scope + " scope, so this step can't be checked."
	s.Next = "Check it in the admin app (" + link + ") or use a key with " + scope + "."
	return s
}

func failed(s checklistStep, err error) checklistStep {
	s.Status, s.Detail = "unknown", "Couldn't check: "+err.Error()
	return s
}

func textOf(res *mcp.CallToolResult) string {
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	return text
}

func itoa(n int) string { return strconv.Itoa(n) }
