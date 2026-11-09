package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Scope names, as issued on Erghi API keys.
const (
	scopeConfigureAI  = "configure_ai"
	scopeIdentity     = "manage_workspace_identity"
	scopeWidgets      = "manage_widgets"
	scopeIntegrations = "manage_integrations"
	scopeAudit        = "view_audit_logs"
	scopeInsights     = "view_insights"
)

var (
	needAI           = []string{scopeConfigureAI}
	needBranding     = []string{scopeConfigureAI, scopeIdentity}
	needWidgets      = []string{scopeWidgets}
	needIntegrations = []string{scopeIntegrations}
	needAudit        = []string{scopeAudit}
	needInsights     = []string{scopeInsights}
)

type empty struct{}

type confirmOnly struct {
	Confirm bool `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed to this change."`
}

// ── Workspace ────────────────────────────────────────────────────────────

func registerWorkspace(r *registry) {
	add(r, read, nil, &mcp.Tool{
		Name:        "get_api_key_info",
		Description: "Show which Erghi workspace this server is connected to and which scopes its API key has. Call this first to know what you can set up.",
	}, func(ctx context.Context, _ empty) (any, error) {
		c, err := r.api.Claims(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"workspaceId": c.WorkspaceID,
			"clientId":    c.ClientID,
			"scopes":      c.Scopes,
			"apiUrl":      r.api.BaseURL(),
			"readOnly":    r.opts.ReadOnly,
		}, nil
	})
}

// ── AI settings ──────────────────────────────────────────────────────────

type modelPreference struct {
	Provider string `json:"provider" jsonschema:"Provider id from list_available_models, e.g. groq"`
	Model    string `json:"model" jsonschema:"Model id from list_available_models"`
}

type updateAISettingsInput struct {
	PreferredProvider   *string           `json:"preferredProvider,omitempty" jsonschema:"Provider of the main model"`
	PreferredModel      *string           `json:"preferredModel,omitempty" jsonschema:"Main model id (see list_available_models)"`
	FallbackChain       []modelPreference `json:"fallbackChain,omitempty" jsonschema:"Ordered models to try when the main one fails"`
	EscalationPolicy    *string           `json:"escalationPolicy,omitempty" jsonschema:"How readily the assistant hands off to a human: Eager, Balanced, Persistent or SelfReliant"`
	SpendCapOverrideUsd *float64          `json:"spendCapOverrideUsd,omitempty" jsonschema:"Monthly AI spend cap in USD (cannot exceed the plan's cap)"`
}

type costUsageInput struct {
	SinceDays int `json:"sinceDays,omitempty" jsonschema:"Look-back window in days (default 30)"`
}

func registerAI(r *registry) {
	add(r, read, needAI, &mcp.Tool{
		Name:        "get_ai_settings",
		Description: "Get the workspace's AI model, fallback chain, escalation policy and spend cap.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/ai-settings", nil, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "list_available_models",
		Description: "List the AI models this workspace's plan can use, with their providers. Models the provider is about to shut down are hidden, and retired ones are removed.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/ai-settings/available-models", nil, nil)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "update_ai_settings",
		Description: "Change the AI model, fallback chain, escalation policy or spend cap. Only the fields you pass change. The assistant's name, tone and instructions are in update_branding.",
	}, func(ctx context.Context, in updateAISettingsInput) (any, error) {
		return r.api.Do(ctx, http.MethodPut, "/api/v1/ai-settings", nil, in)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "get_ai_cost_usage",
		Description: "AI spend for the workspace over a recent window: calls, tokens and cost, plus caching savings. promptCacheHitRate is the share of input tokens the provider served from its cache; cacheHitRate is the share of replies answered from Erghi's own response cache. savedCostUsd is the total saved by both, and costWithoutSavingsUsd is what the spend would have been without them.",
	}, func(ctx context.Context, in costUsageInput) (any, error) {
		days := in.SinceDays
		if days <= 0 {
			days = 30
		}
		return r.api.Do(ctx, http.MethodGet, "/api/v1/ai-settings/cost-usage", url.Values{"sinceDays": {strconv.Itoa(days)}}, nil)
	})
}

// ── Knowledge base ───────────────────────────────────────────────────────

type addDocumentInput struct {
	Title   string `json:"title" jsonschema:"Short document title"`
	Content string `json:"content" jsonschema:"Plain text or Markdown the assistant should know"`
	Source  string `json:"source,omitempty" jsonschema:"Where it came from, e.g. a URL"`
	Kind    string `json:"kind,omitempty" jsonschema:"reference (default): facts the assistant quotes. instruction: staff guidance for a topic (e.g. a refund playbook), retrieved only on turns it applies to and never quoted as a source"`
}

type addFAQInput struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type documentIDInput struct {
	DocumentID string `json:"documentId"`
	Confirm    bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

type searchInput struct {
	Query string `json:"query" jsonschema:"What a visitor might ask"`
}

type crawlInput struct {
	RootURL string `json:"rootUrl" jsonschema:"https URL of the site to index, e.g. https://example.com/help"`
}

type crawlScheduleInput struct {
	RootURL       string `json:"rootUrl" jsonschema:"https URL to re-crawl"`
	IntervalHours int    `json:"intervalHours" jsonschema:"Hours between crawls"`
}

type jobIDInput struct {
	JobID string `json:"jobId"`
}

type scheduleIDInput struct {
	ScheduleID string `json:"scheduleId"`
	Confirm    bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

type playgroundMessage struct {
	Role    string `json:"role" jsonschema:"user or assistant"`
	Content string `json:"content"`
}

type playgroundInput struct {
	Messages []playgroundMessage `json:"messages" jsonschema:"The conversation so far; the last message should be from the user"`
}

func registerKnowledge(r *registry) {
	add(r, read, needAI, &mcp.Tool{
		Name:        "list_knowledge_documents",
		Description: "List the documents in the workspace's knowledge base.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/rag/documents", nil, nil)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "add_knowledge_document",
		Description: "Add a text document to the knowledge base the assistant answers from (product docs, policies, pricing…), or with kind=instruction a topic playbook the assistant follows.",
	}, func(ctx context.Context, in addDocumentInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/rag/documents/ingest", nil, in)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "add_faq",
		Description: "Add one question-and-answer pair to the knowledge base.",
	}, func(ctx context.Context, in addFAQInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/rag/qa", nil, in)
	})
	add(r, destructive, needAI, &mcp.Tool{
		Name:        "delete_knowledge_document",
		Description: "Delete a document from the knowledge base. Needs confirm=true.",
	}, func(ctx context.Context, in documentIDInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		docID, err := id("documentId", in.DocumentID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, "/api/v1/rag/documents/"+docID, nil, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "search_knowledge_base",
		Description: "Search the knowledge base the way the assistant does, to check what it would find for a question.",
	}, func(ctx context.Context, in searchInput) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/rag/search", url.Values{"query": {in.Query}}, nil)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "crawl_website",
		Description: "Start indexing a website into the knowledge base. Returns a job id; poll get_crawl_job.",
	}, func(ctx context.Context, in crawlInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/rag/crawl", nil, in)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "get_crawl_job",
		Description: "Progress of a website crawl started with crawl_website.",
	}, func(ctx context.Context, in jobIDInput) (any, error) {
		jobID, err := id("jobId", in.JobID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, "/api/v1/rag/crawl/"+jobID, nil, nil)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "cancel_crawl_job",
		Description: "Stop a running website crawl.",
	}, func(ctx context.Context, in jobIDInput) (any, error) {
		jobID, err := id("jobId", in.JobID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, "/api/v1/rag/crawl/"+jobID+"/cancel", nil, nil)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "schedule_website_crawl",
		Description: "Re-crawl a website on a schedule so the knowledge base stays current.",
	}, func(ctx context.Context, in crawlScheduleInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/rag/crawl/schedule", nil, in)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "list_crawl_schedules",
		Description: "List scheduled website re-crawls.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/rag/crawl/schedules", nil, nil)
	})
	add(r, destructive, needAI, &mcp.Tool{
		Name:        "disable_crawl_schedule",
		Description: "Stop a scheduled re-crawl. Needs confirm=true.",
	}, func(ctx context.Context, in scheduleIDInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		sid, err := id("scheduleId", in.ScheduleID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, "/api/v1/rag/crawl/schedule/"+sid, nil, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "test_assistant",
		Description: "Ask the workspace's assistant a question in a sandbox (nothing is sent to visitors) and see its reply and the knowledge it used.",
	}, func(ctx context.Context, in playgroundInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/rag/playground", nil, in)
	})
}

// ── Branding and assistant persona ───────────────────────────────────────

type updateBrandingInput struct {
	CompanyName                *string `json:"companyName,omitempty"`
	PrimaryColor               *string `json:"primaryColor,omitempty" jsonschema:"Hex colour, e.g. #3b82f6"`
	SecondaryColor             *string `json:"secondaryColor,omitempty" jsonschema:"Hex colour"`
	PrimaryLanguage            *string `json:"primaryLanguage,omitempty" jsonschema:"Language code, e.g. en or ar"`
	WidgetTheme                *string `json:"widgetTheme,omitempty" jsonschema:"light, dark or auto"`
	WidgetPosition             *string `json:"widgetPosition,omitempty" jsonschema:"bottom-left or bottom-right"`
	WidgetCornerRadius         *int    `json:"widgetCornerRadius,omitempty"`
	EmailFromName              *string `json:"emailFromName,omitempty"`
	TermsURL                   *string `json:"termsUrl,omitempty" jsonschema:"https URL"`
	PrivacyURL                 *string `json:"privacyUrl,omitempty" jsonschema:"https URL"`
	SupportURL                 *string `json:"supportUrl,omitempty" jsonschema:"https URL"`
	AiAssistantName            *string `json:"aiAssistantName,omitempty" jsonschema:"Name visitors see (needs configure_ai)"`
	AiSystemPrompt             *string `json:"aiSystemPrompt,omitempty" jsonschema:"Instructions for the assistant (needs configure_ai). Not secret from visitors."`
	AiTone                     *string `json:"aiTone,omitempty" jsonschema:"needs configure_ai"`
	AiAudience                 *string `json:"aiAudience,omitempty" jsonschema:"needs configure_ai"`
	VisitorContextHints        *string `json:"visitorContextHints,omitempty" jsonschema:"needs configure_ai"`
	HandoffMode                *string `json:"handoffMode,omitempty" jsonschema:"ai_only, queue or auto_assign (needs configure_ai)"`
	EscalationKeywords         *string `json:"escalationKeywords,omitempty" jsonschema:"needs configure_ai"`
	ShowAiLabel                *bool   `json:"showAiLabel,omitempty"`
	ShowAgentName              *bool   `json:"showAgentName,omitempty"`
	MaxConcurrentChatsPerAgent *int    `json:"maxConcurrentChatsPerAgent,omitempty" jsonschema:"1 to 50"`
	QueueWaitingMessage        *string `json:"queueWaitingMessage,omitempty"`
	HandoffMessage             *string `json:"handoffMessage,omitempty"`
	OfflineAgentMessage        *string `json:"offlineAgentMessage,omitempty"`
	VoiceInputEnabled          *bool   `json:"voiceInputEnabled,omitempty"`
	VoiceOutputEnabled         *bool   `json:"voiceOutputEnabled,omitempty"`
}

func registerBranding(r *registry) {
	add(r, read, needBranding, &mcp.Tool{
		Name:        "get_branding",
		Description: "Get the workspace's branding, widget look and assistant persona (name, tone, instructions, hand-off).",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/branding", nil, nil)
	})
	add(r, write, needBranding, &mcp.Tool{
		Name:        "update_branding",
		Description: "Update branding and the assistant persona. Only the fields you pass change. Company/colour/URL fields need manage_workspace_identity; assistant fields (ai*, handoff, escalation) need configure_ai.",
	}, func(ctx context.Context, in updateBrandingInput) (any, error) {
		return r.api.Do(ctx, http.MethodPut, "/api/v1/branding", nil, in)
	})
}

// ── Action policies (what the assistant may do through integrations) ────

type policyIDInput struct {
	PolicyID string `json:"policyId"`
}

// The policy is passed through as an object. It must not be json.RawMessage: that is a
// []byte, so the inferred schema only accepted null or an array and every real policy
// object was refused before it reached the API.
type policyBody struct {
	Policy map[string]any `json:"policy" jsonschema:"Policy object: policyKey (create only), name, description, protectedOperations[], requiredSteps[] (each needs prerequisiteOperationIds; optional successWhen{path,equals}, target{argument,from}, validForMinutes, singleUse; see the policies guide), isActive (create only), requireVerifiedIdentity, argumentRules[]. Operation ids must come from list_protected_operations."`
}

type updatePolicyInput struct {
	PolicyID string         `json:"policyId"`
	Policy   map[string]any `json:"policy" jsonschema:"The whole new definition (not a patch): name, description, protectedOperations[], requiredSteps[], requireVerifiedIdentity, argumentRules[]"`
}

type setPolicyActiveInput struct {
	PolicyID string `json:"policyId"`
	IsActive bool   `json:"isActive"`
}

type policyStatsInput struct {
	Days int `json:"days,omitempty" jsonschema:"Window in days, 1 to 90 (default 7)"`
}

type policyTestCompletion struct {
	Key    string `json:"key" jsonschema:"Step key to treat as completed"`
	Target string `json:"target,omitempty" jsonschema:"For a step with a target: the value it was completed for (e.g. the order id looked up)"`
}

type testPolicyInput struct {
	OperationID      string                 `json:"operationId" jsonschema:"The protected operation to try"`
	Arguments        map[string]any         `json:"arguments,omitempty" jsonschema:"The call's arguments, as the model would send them"`
	IdentityVerified bool                   `json:"identityVerified,omitempty" jsonschema:"Assume the visitor is signed in to the customer's app (a signed identity token)"`
	Identity         map[string]any         `json:"identity,omitempty" jsonschema:"Verified identity claims to assume, e.g. {\"sub\": \"user-1\"}"`
	Metadata         map[string]any         `json:"metadata,omitempty" jsonschema:"Conversation metadata to assume"`
	CompletedSteps   []policyTestCompletion `json:"completedSteps,omitempty" jsonschema:"Steps to treat as already done in the chat (at most 50)"`
}

type deletePolicyInput struct {
	PolicyID string `json:"policyId"`
	Confirm  bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

func registerPolicies(r *registry) {
	const base = "/api/v1/admin/action-policies"
	add(r, read, needAI, &mcp.Tool{
		Name:        "list_action_policies",
		Description: "List the rules that gate what the assistant may do through connected integrations (e.g. verify identity before a refund).",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, base, nil, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "list_protected_operations",
		Description: "List the integration operations an action policy can protect.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, base+"/operations", nil, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "get_action_policy",
		Description: "Get one action policy.",
	}, func(ctx context.Context, in policyIDInput) (any, error) {
		pid, err := id("policyId", in.PolicyID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, base+"/"+pid, nil, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "get_action_policy_stats",
		Description: "Allowed and refused protected calls per action policy over the last days, with refusals broken down by the step that refused them. A step that refuses almost everything is often misconfigured (a successWhen path that never matches, a target the prerequisite never carries).",
	}, func(ctx context.Context, in policyStatsInput) (any, error) {
		days := in.Days
		if days == 0 {
			days = 7
		}
		if days < 1 || days > 90 {
			return nil, fmt.Errorf("days must be between 1 and 90")
		}
		return r.api.Do(ctx, http.MethodGet, base+"/stats", url.Values{"days": {strconv.Itoa(days)}}, nil)
	})
	add(r, read, needAI, &mcp.Tool{
		Name:        "test_action_policy",
		Description: "Dry-run the policy gate: what the workspace's active policies would decide for one protected call, given the identity, metadata and completed steps you describe. Nothing is called, changed or audited. Use it after create_action_policy or update_action_policy to check the policy refuses and allows what you meant.",
	}, func(ctx context.Context, in testPolicyInput) (any, error) {
		if strings.TrimSpace(in.OperationID) == "" {
			return nil, fmt.Errorf("operationId is required")
		}
		if len(in.CompletedSteps) > 50 {
			return nil, fmt.Errorf("completedSteps can list at most 50 steps")
		}
		return r.api.Do(ctx, http.MethodPost, base+"/test", nil, in)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "create_action_policy",
		Description: "Create an action policy: a hard limit Erghi checks before a protected integration call runs. Read get_erghi_guide topic policies first and take operation ids from list_protected_operations.",
	}, func(ctx context.Context, in policyBody) (any, error) {
		if len(in.Policy) == 0 {
			return nil, fmt.Errorf("policy is required")
		}
		return r.api.Do(ctx, http.MethodPost, base, nil, in.Policy)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "update_action_policy",
		Description: "Replace an action policy's whole definition (not a patch): read it with get_action_policy and send every field you want to keep.",
	}, func(ctx context.Context, in updatePolicyInput) (any, error) {
		if len(in.Policy) == 0 {
			return nil, fmt.Errorf("policy is required")
		}
		pid, err := id("policyId", in.PolicyID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, base+"/"+pid, nil, in.Policy)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "set_action_policy_active",
		Description: "Turn an action policy on or off.",
	}, func(ctx context.Context, in setPolicyActiveInput) (any, error) {
		pid, err := id("policyId", in.PolicyID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPatch, base+"/"+pid+"/active", nil, map[string]bool{"isActive": in.IsActive})
	})
	add(r, destructive, needAI, &mcp.Tool{
		Name:        "delete_action_policy",
		Description: "Delete an action policy. Needs confirm=true.",
	}, func(ctx context.Context, in deletePolicyInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		pid, err := id("policyId", in.PolicyID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, base+"/"+pid, nil, nil)
	})
}

// ── Channels ─────────────────────────────────────────────────────────────

type channelInput struct {
	Channel string `json:"channel" jsonschema:"telegram, slack or whatsapp"`
}

type disconnectChannelInput struct {
	Channel string `json:"channel" jsonschema:"telegram, slack or whatsapp"`
	Confirm bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

type telegramInput struct {
	BotToken string `json:"botToken" jsonschema:"Token from @BotFather. Ask the user for it; never invent one."`
}

type slackInput struct {
	WebhookURL string `json:"webhookUrl" jsonschema:"Slack incoming-webhook URL for hand-off notifications"`
}

type whatsAppInput struct {
	AccessToken   string `json:"accessToken" jsonschema:"WhatsApp Cloud API access token"`
	PhoneNumberID string `json:"phoneNumberId" jsonschema:"WhatsApp Cloud API phone number id"`
}

func channelPath(channel string) (string, error) {
	switch channel {
	case "telegram", "slack", "whatsapp":
		return "/api/v1/channels/" + channel, nil
	}
	return "", fmt.Errorf("channel must be telegram, slack or whatsapp, got %q", channel)
}

func registerChannels(r *registry) {
	add(r, read, needAI, &mcp.Tool{
		Name:        "get_channel_status",
		Description: "Whether Telegram, Slack or WhatsApp is connected.",
	}, func(ctx context.Context, in channelInput) (any, error) {
		p, err := channelPath(in.Channel)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p, nil, nil)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "connect_telegram",
		Description: "Connect a Telegram bot so the assistant answers Telegram messages.",
	}, func(ctx context.Context, in telegramInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/channels/telegram/connect", nil, in)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "connect_slack",
		Description: "Send hand-off notifications to a Slack channel.",
	}, func(ctx context.Context, in slackInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/channels/slack/connect", nil, in)
	})
	add(r, write, needAI, &mcp.Tool{
		Name:        "connect_whatsapp",
		Description: "Connect a WhatsApp Cloud API number so the assistant answers WhatsApp messages.",
	}, func(ctx context.Context, in whatsAppInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/v1/channels/whatsapp/connect", nil, in)
	})
	add(r, destructive, needAI, &mcp.Tool{
		Name:        "disconnect_channel",
		Description: "Disconnect Telegram, Slack or WhatsApp. Needs confirm=true.",
	}, func(ctx context.Context, in disconnectChannelInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := channelPath(in.Channel)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, p, nil, nil)
	})
}

// ── Widgets ──────────────────────────────────────────────────────────────

type widgetConfiguration struct {
	InheritWorkspaceBranding *bool             `json:"inheritWorkspaceBranding,omitempty" jsonschema:"Use the workspace branding colours (default true)"`
	PrimaryColor             *string           `json:"primaryColor,omitempty"`
	SecondaryColor           *string           `json:"secondaryColor,omitempty"`
	WelcomeMessage           *string           `json:"welcomeMessage,omitempty"`
	EnableAI                 *bool             `json:"enableAI,omitempty"`
	RequireEmail             *bool             `json:"requireEmail,omitempty"`
	AllowedDomains           []string          `json:"allowedDomains,omitempty" jsonschema:"Sites allowed to embed the widget, e.g. example.com"`
	Translations             map[string]string `json:"translations,omitempty"`
}

type createWidgetInput struct {
	Name          string               `json:"name"`
	Slug          string               `json:"slug" jsonschema:"URL-safe identifier, lowercase with dashes"`
	Configuration *widgetConfiguration `json:"configuration,omitempty"`
}

type widgetIDInput struct {
	WidgetID string `json:"widgetId"`
}

type updateWidgetInput struct {
	WidgetID      string               `json:"widgetId"`
	Name          *string              `json:"name,omitempty"`
	IsActive      *bool                `json:"isActive,omitempty" jsonschema:"Set true to turn a deactivated widget back on"`
	Configuration *widgetConfiguration `json:"configuration,omitempty" jsonschema:"Replaces the widget configuration; read it with get_widget first and send it back changed"`
}

type deactivateWidgetInput struct {
	WidgetID string `json:"widgetId"`
	Confirm  bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

func registerWidgets(r *registry) {
	add(r, read, needWidgets, &mcp.Tool{
		Name:        "list_widgets",
		Description: "List the workspace's chat widgets.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/widgets", nil, nil)
	})
	add(r, read, needWidgets, &mcp.Tool{
		Name:        "get_widget",
		Description: "Get one widget and its configuration.",
	}, func(ctx context.Context, in widgetIDInput) (any, error) {
		wid, err := id("widgetId", in.WidgetID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, "/api/widgets/"+wid, nil, nil)
	})
	add(r, write, needWidgets, &mcp.Tool{
		Name:        "create_widget",
		Description: "Create a chat widget for a website. Set allowedDomains to the sites that will embed it, then use get_widget_embed_code.",
	}, func(ctx context.Context, in createWidgetInput) (any, error) {
		return r.api.Do(ctx, http.MethodPost, "/api/widgets", nil, in)
	})
	add(r, write, needWidgets, &mcp.Tool{
		Name:        "update_widget",
		Description: "Rename a widget or change its configuration (welcome message, colours, allowed domains…).",
	}, func(ctx context.Context, in updateWidgetInput) (any, error) {
		wid, err := id("widgetId", in.WidgetID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, "/api/widgets/"+wid, nil, map[string]any{
			"name":          in.Name,
			"isActive":      in.IsActive,
			"configuration": in.Configuration,
		})
	})
	add(r, destructive, needWidgets, &mcp.Tool{
		Name:        "deactivate_widget",
		Description: "Turn a widget off on every site that embeds it (conversation history is kept). Needs confirm=true.",
	}, func(ctx context.Context, in deactivateWidgetInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		wid, err := id("widgetId", in.WidgetID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, "/api/widgets/"+wid, nil, nil)
	})
	add(r, read, needWidgets, &mcp.Tool{
		Name:        "get_widget_embed_code",
		Description: "The <script> tag to paste before </body> on the website to show a widget.",
	}, func(_ context.Context, in widgetIDInput) (any, error) {
		wid, err := id("widgetId", in.WidgetID)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf(`<script src="%s/widget.js" data-widget-id="%s" data-api-url="%s"></script>`,
			r.opts.WidgetHost, wid, r.api.BaseURL()), nil
	})
}

// ── Audit log ────────────────────────────────────────────────────────────

type auditInput struct {
	Category string `json:"category,omitempty" jsonschema:"Optional category filter, e.g. agent, billing, ai"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max entries (default 50, max 200)"`
	Offset   int    `json:"offset,omitempty"`
}

func registerAudit(r *registry) {
	add(r, read, needAudit, &mcp.Tool{
		Name:        "list_audit_logs",
		Description: "Recent security and configuration changes in the workspace, including those made through this API key.",
	}, func(ctx context.Context, in auditInput) (any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		q := url.Values{"limit": {strconv.Itoa(min(limit, 200))}, "offset": {strconv.Itoa(max(in.Offset, 0))}}
		if in.Category != "" {
			q.Set("category", in.Category)
		}
		return r.api.Do(ctx, http.MethodGet, "/api/v1/admin/audit-logs", q, nil)
	})
}

// ── Conversation insights ────────────────────────────────────────────────

// insightFilter mirrors the filters of GET /api/v1/insights/*. The API validates
// the values and answers 400 invalid_filter for a bad one.
type insightFilter struct {
	From          string   `json:"from,omitempty" jsonschema:"Only conversations classified at or after this time (ISO 8601, e.g. 2026-11-01T00:00:00Z)"`
	To            string   `json:"to,omitempty" jsonschema:"Only conversations classified before this time (ISO 8601). Must not be earlier than from"`
	Channel       string   `json:"channel,omitempty" jsonschema:"Conversation channel, e.g. widget, telegram, slack, whatsapp"`
	Category      string   `json:"category,omitempty" jsonschema:"Exact category label, as returned in the summary"`
	Topic         string   `json:"topic,omitempty" jsonschema:"Exact topic label, as returned in the summary"`
	Sentiment     string   `json:"sentiment,omitempty" jsonschema:"positive, neutral or negative"`
	Outcome       string   `json:"outcome,omitempty" jsonschema:"resolved_by_ai, resolved_by_agent, handed_off_unresolved, abandoned or spam_or_empty"`
	MinConfidence *float64 `json:"minConfidence,omitempty" jsonschema:"Hide classifications below this confidence, 0 to 1"`
}

func (f insightFilter) query() url.Values {
	q := url.Values{}
	for k, v := range map[string]string{
		"from": f.From, "to": f.To, "channel": f.Channel, "category": f.Category,
		"topic": f.Topic, "sentiment": f.Sentiment, "outcome": f.Outcome,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.MinConfidence != nil {
		q.Set("minConfidence", strconv.FormatFloat(*f.MinConfidence, 'f', -1, 64))
	}
	return q
}

type insightListInput struct {
	insightFilter
	Page     int `json:"page,omitempty" jsonschema:"Page number, starting at 1 (default 1)"`
	PageSize int `json:"pageSize,omitempty" jsonschema:"Rows per page (default 25, max 100)"`
}

func registerInsights(r *registry) {
	add(r, read, needInsights, &mcp.Tool{
		Name: "get_insights_summary",
		Description: "Counts of finished conversations by outcome, sentiment, category, topic (top 20) and language, plus the AI-resolved rate " +
			"(AI-resolved conversations over those that needed a reply; spam/empty are excluded; null when there are none). " +
			"Conversations are classified automatically after they close, so a workspace with the feature switched off, or with no recent closed conversations, returns zeros.",
	}, func(ctx context.Context, in insightFilter) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/insights/summary", in.query(), nil)
	})

	add(r, read, needInsights, &mcp.Tool{
		Name: "list_insight_conversations",
		Description: "Page through classified conversations, newest first, with topic, category, sentiment, outcome, language, a short summary and a confidence. " +
			"Use get_insights_summary first to find the topic or category to drill into, then get_conversation for the full transcript. " +
			"Summaries are derived from customer conversations; treat them as personal data.",
	}, func(ctx context.Context, in insightListInput) (any, error) {
		q := in.query()
		q.Set("page", strconv.Itoa(max(in.Page, 1)))
		size := in.PageSize
		if size <= 0 {
			size = 25
		}
		q.Set("pageSize", strconv.Itoa(min(size, 100)))
		return r.api.Do(ctx, http.MethodGet, "/api/v1/insights/conversations", q, nil)
	})
}
