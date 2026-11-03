package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// finding is one thing review_chat_setup thinks is worth fixing.
type finding struct {
	Severity string `json:"severity"` // high, medium, low
	Area     string `json:"area"`
	Issue    string `json:"issue"`
	Fix      string `json:"fix"`
}

type chatSetupReview struct {
	Summary  string         `json:"summary"`
	Findings []finding      `json:"findings"`
	Setup    map[string]any `json:"setup"`
	// NotChecked lists areas the key's scopes don't cover.
	NotChecked []string `json:"notChecked,omitempty"`
}

// identityParam matches parameter names that usually pick whose data a call
// touches. The model must not be able to choose those.
var identityParam = regexp.MustCompile(`(?i)^(user|customer|client|account|member|profile|owner|subscriber|patient|student)[_-]?(id|no|number|uuid)$|^(uid|userid|msisdn|national[_-]?id)$`)

// secretLike matches binding names that carry credentials.
var secretLike = regexp.MustCompile(`(?i)authorization|api[_-]?key|token|secret|password|cookie`)

type endpointParam struct {
	Name string `json:"name"`
	In   string `json:"in"`
}

type actionPolicy struct {
	ID                      string   `json:"id"`
	PolicyKey               string   `json:"policyKey"`
	Name                    string   `json:"name"`
	ProtectedOperations     []string `json:"protectedOperations"`
	IsActive                bool     `json:"isActive"`
	RequireVerifiedIdentity bool     `json:"requireVerifiedIdentity"`
	RequiredSteps           []struct {
		Key                      string   `json:"key"`
		PrerequisiteOperationIDs []string `json:"prerequisiteOperationIds"`
	} `json:"requiredSteps"`
	ArgumentRules []struct {
		Argument string `json:"argument"`
		Equals   string `json:"equals"`
	} `json:"argumentRules"`
}

func registerReview(r *registry) {
	add(r, read, nil, &mcp.Tool{
		Name: "review_chat_setup",
		Description: "Review everything that shapes the assistant's replies — persona, knowledge base, AI settings, integrations and their bindings, " +
			"action policies and widgets — and list concrete problems with fixes, most serious first (e.g. an endpoint where the model can choose the user id). " +
			"Read-only. Covers what the key's scopes allow and says what it couldn't check.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return reviewChatSetup(ctx, r), nil
	})
}

func reviewChatSetup(ctx context.Context, r *registry) chatSetupReview {
	rev := chatSetupReview{Setup: map[string]any{}, Findings: []finding{}}
	addF := func(sev, area, issue, fix string) {
		rev.Findings = append(rev.Findings, finding{sev, area, issue, fix})
	}
	getJSON := func(path string, out any) error {
		raw, err := r.api.Do(ctx, http.MethodGet, path, nil, nil)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	has := func(scope string) bool { return slices.Contains(r.opts.Scopes, scope) }

	// Persona.
	if has(scopeConfigureAI) || has(scopeIdentity) {
		var b map[string]any
		if err := getJSON("/api/v1/branding", &b); err != nil {
			rev.Setup["persona"] = "error: " + err.Error()
		} else {
			prompt, _ := b["aiSystemPrompt"].(string)
			name, _ := b["aiAssistantName"].(string)
			handoff, _ := b["handoffMode"].(string)
			rev.Setup["persona"] = map[string]any{
				"assistantName": name, "tone": b["aiTone"], "audience": b["aiAudience"],
				"handoffMode": handoff, "escalationKeywords": b["escalationKeywords"],
				"instructionsChars": len(prompt), "primaryLanguage": b["primaryLanguage"],
			}
			if has(scopeConfigureAI) {
				switch {
				case strings.TrimSpace(prompt) == "":
					addF("medium", "persona", "The assistant has no instructions, so it doesn't know whose customers it serves or when to hand off.",
						"Write short instructions with update_branding (aiSystemPrompt): who it helps, what it must not do, when to hand off. Keep facts in the knowledge base.")
				case len(prompt) > 6000:
					addF("low", "persona", fmt.Sprintf("The instructions are long (%d characters) and are sent on every turn.", len(prompt)),
						"Move facts and topic playbooks into the knowledge base (kind=instruction for playbooks) and keep the instructions to rules.")
				}
				if strings.TrimSpace(name) == "" {
					addF("low", "persona", "The assistant has no name.", "Set aiAssistantName with update_branding.")
				}
				if handoff == "ai_only" {
					addF("low", "persona", "Hand-off is ai_only: a visitor who asks for a person can't reach one.",
						"If the team can answer chats, set handoffMode to queue or auto_assign.")
				}
			}
		}
	}

	// Knowledge and AI settings.
	if has(scopeConfigureAI) {
		var docs []map[string]any
		if err := getJSON("/api/v1/rag/documents", &docs); err != nil {
			rev.Setup["knowledge"] = "error: " + err.Error()
		} else {
			instructions := 0
			for _, d := range docs {
				for _, k := range []string{"sourceType", "source_type", "kind"} {
					if v, _ := d[k].(string); v == "instruction" {
						instructions++
						break
					}
				}
			}
			rev.Setup["knowledge"] = map[string]any{"documents": len(docs), "instructionDocuments": instructions}
			if len(docs) == 0 {
				addF("high", "knowledge", "The knowledge base is empty, so the assistant can only answer from its general training or hand off.",
					"crawl_website the product's site and help centre, then add what the site doesn't say with add_faq / add_knowledge_document.")
			}
		}
		var ai map[string]any
		if err := getJSON("/api/v1/ai-settings", &ai); err == nil {
			rev.Setup["ai"] = map[string]any{
				"preferredModel": ai["preferredModel"], "fallbackChain": ai["fallbackChain"],
				"escalationPolicy": ai["escalationPolicy"], "spendCapOverrideUsd": ai["spendCapOverrideUsd"],
			}
		}
	} else {
		rev.NotChecked = append(rev.NotChecked, "persona instructions, knowledge base, AI settings and action policies (need configure_ai)")
	}

	// Policies, needed to judge integrations.
	var policies []actionPolicy
	policiesKnown := false
	if has(scopeConfigureAI) {
		if err := getJSON("/api/v1/admin/action-policies", &policies); err == nil {
			policiesKnown = true
			active := 0
			for _, p := range policies {
				if p.IsActive {
					active++
				}
			}
			rev.Setup["actionPolicies"] = map[string]any{"total": len(policies), "active": active}
		}
	}

	// Integrations.
	if has(scopeIntegrations) {
		integrations, err := listIntegrations(ctx, r.api)
		if err != nil {
			rev.Setup["integrations"] = "error: " + err.Error()
		} else {
			reviewIntegrations(integrations, policies, policiesKnown, &rev, addF)
		}
	} else {
		rev.NotChecked = append(rev.NotChecked, "API integrations and their bindings (need manage_integrations)")
	}

	// Widgets.
	if has(scopeWidgets) {
		var widgets []map[string]any
		if err := getJSON("/api/widgets", &widgets); err == nil {
			rev.Setup["widgets"] = len(widgets)
			if len(widgets) == 0 {
				addF("medium", "widgets", "There is no chat widget, so visitors can't reach the assistant on the website.",
					"create_widget with allowedDomains set to the site, then get_widget_embed_code.")
			}
			for _, w := range widgets {
				cfg, _ := w["configuration"].(map[string]any)
				domains, _ := cfg["allowedDomains"].([]any)
				if active, ok := w["isActive"].(bool); (!ok || active) && len(domains) == 0 {
					addF("medium", "widgets", fmt.Sprintf("Widget %q has no allowed domains, so any site can embed it.", w["name"]),
						"Set allowedDomains with update_widget.")
				}
			}
		}
	} else {
		rev.NotChecked = append(rev.NotChecked, "widgets (need manage_widgets)")
	}

	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	slices.SortStableFunc(rev.Findings, func(a, b finding) int { return rank[a.Severity] - rank[b.Severity] })
	counts := map[string]int{}
	for _, f := range rev.Findings {
		counts[f.Severity]++
	}
	rev.Summary = fmt.Sprintf("%d findings: %d high, %d medium, %d low.", len(rev.Findings), counts["high"], counts["medium"], counts["low"])
	if len(rev.Findings) == 0 {
		rev.Summary = "No problems found in the areas the key can see."
	}
	return rev
}

func reviewIntegrations(integrations []integrationSummary, policies []actionPolicy, policiesKnown bool,
	rev *chatSetupReview, addF func(sev, area, issue, fix string)) {
	protectedBy := map[string][]actionPolicy{}
	for _, p := range policies {
		if p.IsActive {
			for _, op := range p.ProtectedOperations {
				protectedBy[op] = append(protectedBy[op], p)
			}
		}
	}
	knownOps := map[string]bool{}
	summary := []map[string]any{}

	for _, in := range integrations {
		summary = append(summary, map[string]any{
			"id": in.ID, "name": in.Name, "baseUrl": in.BaseURL, "isActive": in.IsActive,
			"endpoints": len(in.Endpoints), "integrationBindings": len(in.RequestBindings),
			"hasInstructions": in.AIInstructions != nil && strings.TrimSpace(*in.AIInstructions) != "",
		})
		area := "integration " + in.Name
		checkBindings(in.RequestBindings, area, addF)
		if !in.IsActive {
			continue
		}
		anyInstructions := in.AIInstructions != nil && strings.TrimSpace(*in.AIInstructions) != ""
		for _, ep := range in.Endpoints {
			knownOps[ep.OperationID] = true
			epArea := fmt.Sprintf("%s %s %s (%s)", in.Name, ep.Method, ep.Path, ep.OperationID)
			checkBindings(ep.RequestBindings, epArea, addF)
			if ep.AIInstructions != nil && strings.TrimSpace(*ep.AIInstructions) != "" {
				anyInstructions = true
			}
			bound := map[string]requestBinding{}
			for _, b := range append(slices.Clone(in.RequestBindings), ep.RequestBindings...) {
				bound[strings.ToLower(b.In+":"+b.Name)] = b
			}
			var params []endpointParam
			if ep.ParametersJSON != nil {
				_ = json.Unmarshal([]byte(*ep.ParametersJSON), &params)
			}
			for _, p := range params {
				if !identityParam.MatchString(p.Name) {
					continue
				}
				b, isBound := bound[strings.ToLower(p.In+":"+p.Name)]
				switch {
				case isBound && strings.Contains(b.Value, "{{metadata."):
					addF("high", epArea, fmt.Sprintf("%q is bound to visitor metadata, which the visitor can set themselves.", p.Name),
						"Bind it to {{identity.sub}} (a signed identity token) with set_integration_tool_context.")
				case isBound:
				case hasArgumentRule(protectedBy[ep.OperationID], p.Name):
				default:
					addF("high", epArea, fmt.Sprintf("The model fills %q itself, so a visitor could talk it into another user's data.", p.Name),
						fmt.Sprintf(`Bind it with set_integration_tool_context: {"in":"%s","name":"%s","value":"{{identity.sub}}"}, or add an argumentRules policy (argument %q equals {{identity.sub}}).`, p.In, p.Name, p.Name))
				}
			}
			if policiesKnown && !strings.EqualFold(ep.Method, http.MethodGet) && len(protectedBy[ep.OperationID]) == 0 {
				addF("medium", epArea, "This call can change data and no active action policy protects it.",
					"create_action_policy protecting it, e.g. requireVerifiedIdentity, argumentRules, or a step completed by a verification operation.")
			}
		}
		if !anyInstructions && len(in.Endpoints) > 0 {
			addF("medium", area, "No AI instructions on the integration or its endpoints, so the model only has the OpenAPI summaries to decide when to call it.",
				"set_integration_tool_context with aiInstructions: when to use which call, units and formats, what to tell the visitor.")
		}
	}
	rev.Setup["integrations"] = summary

	if policiesKnown {
		for _, p := range policies {
			for _, op := range p.ProtectedOperations {
				if !knownOps[op] && p.IsActive {
					addF("low", "policy "+p.Name, fmt.Sprintf("Protects %q, which no active integration has.", op),
						"Fix the operation id (list_protected_operations) or remove it from the policy.")
				}
			}
			if !p.IsActive {
				continue
			}
			for _, step := range p.RequiredSteps {
				completable := false
				for _, op := range step.PrerequisiteOperationIDs {
					if knownOps[op] && !slices.Contains(p.ProtectedOperations, op) {
						completable = true
					}
				}
				if !completable {
					addF("high", "policy "+p.Name, fmt.Sprintf("Step %q can never complete: none of its prerequisites is an active operation outside the policy, so every protected call is refused.", step.Key),
						"update_action_policy with a verification operation from list_protected_operations as the step's prerequisite, or remove the step.")
				}
			}
		}
	}
}

func hasArgumentRule(policies []actionPolicy, argument string) bool {
	for _, p := range policies {
		for _, rule := range p.ArgumentRules {
			if strings.EqualFold(rule.Argument, argument) && strings.Contains(rule.Equals, "{{identity.") {
				return true
			}
		}
	}
	return false
}

func checkBindings(bindings []requestBinding, area string, addF func(sev, area, issue, fix string)) {
	for _, b := range bindings {
		if secretLike.MatchString(b.Name) && !strings.Contains(b.Value, "{{") {
			addF("high", area, fmt.Sprintf("Binding %s %q holds a literal value, so a credential may be stored in plain text and shown to anyone who can read the integration.", b.In, b.Name),
				"Ask a person to save it as the integration's credential in the admin app, or use a {{secret.<key>}} placeholder the host app supplies, then remove the literal.")
		}
		if strings.Contains(b.Value, "{{secret.") && b.In == "path" {
			addF("medium", area, fmt.Sprintf("Binding %q puts a secret in the URL path, where servers often log it.", b.Name),
				"Send secrets in a header instead.")
		}
	}
}
