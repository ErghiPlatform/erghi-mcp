package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Inbox settings (manage_inbox_settings): labels, saved replies, teams,
// routing, business hours, SLAs, automation rules and the CSAT survey. These
// configure how conversations are organised; they never read or change a
// conversation's content.

// inboxPath builds /api/v1/inboxes/{type}/{id}. An inbox is a widget or a
// messaging channel.
func inboxPath(inboxType, inboxID string) (string, error) {
	if inboxType != "widget" && inboxType != "channel" {
		return "", fmt.Errorf("inboxType must be widget or channel, got %q", inboxType)
	}
	x, err := id("inboxId", inboxID)
	if err != nil {
		return "", err
	}
	return "/api/v1/inboxes/" + inboxType + "/" + x, nil
}

type confirmIDInput struct {
	ID      string `json:"id"`
	Confirm bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

type idInput struct {
	ID string `json:"id"`
}

// ── Labels and saved replies ─────────────────────────────────────────────

type labelFields struct {
	Name        string  `json:"name"`
	Color       *string `json:"color,omitempty" jsonschema:"Hex colour, e.g. #2563eb"`
	Description *string `json:"description,omitempty"`
}

type updateLabelInput struct {
	ID string `json:"id"`
	labelFields
}

type cannedFields struct {
	ShortCode string `json:"shortCode" jsonschema:"What agents type after / to insert it, e.g. refund"`
	Content   string `json:"content" jsonschema:"The reply text; may use variables such as {{contact.name}}"`
}

type updateCannedInput struct {
	ID string `json:"id"`
	cannedFields
}

type searchCannedInput struct {
	Search string `json:"search,omitempty"`
}

// ── Teams and routing ────────────────────────────────────────────────────

type teamFields struct {
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	AllowAutoAssign *bool   `json:"allowAutoAssign,omitempty" jsonschema:"Whether new conversations of the team are assigned to its members automatically (default true)"`
}

type updateTeamInput struct {
	ID string `json:"id"`
	teamFields
}

type memberInput struct {
	ID      string `json:"id" jsonschema:"The team id"`
	UserID  string `json:"userId" jsonschema:"A workspace member, from list_assignable_members"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Needed to remove a member. Only set it after the user has agreed."`
}

type inboxInput struct {
	InboxType string `json:"inboxType" jsonschema:"widget or channel"`
	InboxID   string `json:"inboxId" jsonschema:"The widget id, or the channel id from get_channel_status"`
}

type inboxMemberInput struct {
	inboxInput
	UserID  string `json:"userId"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Needed to remove a member. Only set it after the user has agreed."`
}

type assignmentSettingsInput struct {
	Mode                 string `json:"mode" jsonschema:"legacy, round_robin or balanced"`
	CapacityLimitEnabled *bool  `json:"capacityLimitEnabled,omitempty" jsonschema:"Respect each agent's maximum open conversations (default true)"`
	AwayReassignMinutes  *int   `json:"awayReassignMinutes,omitempty" jsonschema:"Give an away agent's conversations to someone else after this many minutes; null never does"`
}

// ── Business hours and SLAs ──────────────────────────────────────────────

type businessInterval struct {
	Start string `json:"start" jsonschema:"HH:mm, 24h, local time"`
	End   string `json:"end" jsonschema:"HH:mm, exclusive; 24:00 allowed"`
}

type businessDay struct {
	DayOfWeek int                `json:"dayOfWeek" jsonschema:"0 = Sunday … 6 = Saturday"`
	Intervals []businessInterval `json:"intervals" jsonschema:"Open intervals; none means closed"`
}

type businessHoursFields struct {
	Enabled  bool          `json:"enabled"`
	TimeZone string        `json:"timeZone" jsonschema:"IANA time zone, e.g. Africa/Cairo"`
	Schedule []businessDay `json:"schedule,omitempty" jsonschema:"Opening hours per weekday; a missing day is closed"`
	Holidays []string      `json:"holidays,omitempty" jsonschema:"Closed dates, YYYY-MM-DD"`
}

type optionalInboxInput struct {
	InboxType string `json:"inboxType,omitempty" jsonschema:"widget or channel; leave out for the workspace's own hours"`
	InboxID   string `json:"inboxId,omitempty"`
}

func (o optionalInboxInput) path() (string, error) {
	if o.InboxType == "" && o.InboxID == "" {
		return "/api/v1/business-hours", nil
	}
	p, err := inboxPath(o.InboxType, o.InboxID)
	if err != nil {
		return "", err
	}
	return p + "/business-hours", nil
}

type setBusinessHoursInput struct {
	optionalInboxInput
	businessHoursFields
}

type clearInboxHoursInput struct {
	inboxInput
	Confirm bool `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

type slaFields struct {
	Name                 string   `json:"name"`
	FirstResponseMinutes *int     `json:"firstResponseMinutes,omitempty" jsonschema:"Target time to the first reply"`
	NextResponseMinutes  *int     `json:"nextResponseMinutes,omitempty" jsonschema:"Target time to each later reply"`
	ResolutionMinutes    *int     `json:"resolutionMinutes,omitempty" jsonschema:"Target time to resolve"`
	BusinessHoursOnly    bool     `json:"businessHoursOnly,omitempty" jsonschema:"Count only business hours"`
	MatchPriorities      []string `json:"matchPriorities,omitempty" jsonschema:"Applies to these priorities only (low, medium, high, urgent); empty means all"`
	MatchTeamID          *string  `json:"matchTeamId,omitempty"`
	MatchInboxType       *string  `json:"matchInboxType,omitempty" jsonschema:"widget or channel"`
	MatchInboxID         *string  `json:"matchInboxId,omitempty"`
	IsDefault            bool     `json:"isDefault,omitempty" jsonschema:"Used when no other policy matches"`
	Active               *bool    `json:"active,omitempty" jsonschema:"Default true"`
}

func (s slaFields) validate() error {
	for name, v := range map[string]*string{"matchTeamId": s.MatchTeamID, "matchInboxId": s.MatchInboxID} {
		if v != nil {
			if _, err := id(name, *v); err != nil {
				return err
			}
		}
	}
	return nil
}

type updateSlaInput struct {
	ID string `json:"id"`
	slaFields
}

// ── Automation rules ─────────────────────────────────────────────────────

type automationCondition struct {
	Field    string  `json:"field" jsonschema:"status, priority, channel, widget, team, assignee, label, message_text, message_sender, business_hours, contact_email or changed"`
	Operator string  `json:"operator" jsonschema:"is, is_not, is_set, is_not_set, has, has_not, contains, not_contains, starts_with or includes"`
	Value    *string `json:"value,omitempty"`
}

type automationConditions struct {
	Match      string                `json:"match" jsonschema:"all (AND) or any (OR)"`
	Conditions []automationCondition `json:"conditions,omitempty" jsonschema:"No conditions means the rule always matches"`
}

type macroAction struct {
	Type             string  `json:"type" jsonschema:"add_label, remove_label, assign, unassign, set_status, set_priority, snooze, add_note, send_canned_reply or set_team"`
	LabelID          *string `json:"labelId,omitempty"`
	UserID           *string `json:"userId,omitempty"`
	Status           *string `json:"status,omitempty" jsonschema:"open, pending or resolved"`
	Priority         *string `json:"priority,omitempty" jsonschema:"none, low, medium, high or urgent"`
	SnoozeMinutes    *int    `json:"snoozeMinutes,omitempty"`
	UntilNextReply   bool    `json:"untilNextReply,omitempty"`
	Content          *string `json:"content,omitempty" jsonschema:"add_note: the internal note"`
	CannedResponseID *string `json:"cannedResponseId,omitempty" jsonschema:"send_canned_reply: sends this saved reply to the customer"`
	TeamID           *string `json:"teamId,omitempty"`
}

type automationFields struct {
	Name           string                `json:"name"`
	Trigger        string                `json:"trigger" jsonschema:"conversation_created, conversation_updated, message_created or conversation_idle"`
	IdleMinutes    *int                  `json:"idleMinutes,omitempty" jsonschema:"conversation_idle only: minutes without a new message (5 to 43200)"`
	Conditions     *automationConditions `json:"conditions,omitempty"`
	Actions        []macroAction         `json:"actions"`
	IsActive       *bool                 `json:"isActive,omitempty" jsonschema:"Default true"`
	DryRun         bool                  `json:"dryRun,omitempty" jsonschema:"Record what the rule would do without changing anything; good for trying a new rule"`
	StopProcessing bool                  `json:"stopProcessing,omitempty" jsonschema:"When this rule matches, later rules for the same event do not run"`
}

func (a automationFields) validate() error {
	for i, act := range a.Actions {
		for name, v := range map[string]*string{
			"labelId": act.LabelID, "userId": act.UserID, "cannedResponseId": act.CannedResponseID, "teamId": act.TeamID,
		} {
			if v != nil {
				if _, err := id(fmt.Sprintf("actions[%d].%s", i, name), *v); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type updateAutomationInput struct {
	ID string `json:"id"`
	automationFields
}

type reorderInput struct {
	IDs []string `json:"ids" jsonschema:"Every rule id of the workspace, in the new order"`
}

type testAutomationInput struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId" jsonschema:"Conversation to check the rule's conditions against; nothing is changed"`
}

type automationRunsInput struct {
	RuleID         string `json:"ruleId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	Before         string `json:"before,omitempty" jsonschema:"ISO 8601; runs before this time, for paging"`
	Limit          int    `json:"limit,omitempty" jsonschema:"At most this many (default 50)"`
}

// ── CSAT ─────────────────────────────────────────────────────────────────

type csatSettingsInput struct {
	Enabled     bool     `json:"enabled"`
	Question    *string  `json:"question,omitempty" jsonschema:"The survey question; null uses the default"`
	Channels    []string `json:"channels,omitempty" jsonschema:"Where the survey is sent, e.g. widget, telegram; empty means everywhere"`
	ExpiryHours int      `json:"expiryHours" jsonschema:"How long a customer can still answer"`
}

func registerInbox(r *registry) {
	registerLabelsAndReplies(r)
	registerTeamsAndRouting(r)
	registerHoursAndSla(r)
	registerAutomations(r)

	add(r, read, needInbox, &mcp.Tool{
		Name:        "get_csat_settings",
		Description: "The customer-satisfaction survey sent after a conversation is resolved.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/csat/settings", nil, nil)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "update_csat_settings",
		Description: "Turn the satisfaction survey on or off and set its question, channels and expiry. Every field is replaced.",
	}, func(ctx context.Context, in csatSettingsInput) (any, error) {
		return r.api.Do(ctx, http.MethodPut, "/api/v1/csat/settings", nil, in)
	})
}

// crudDelete and crudGet register the delete and get tools of a settings collection.
func crudDelete(r *registry, name, what, base string) {
	add(r, destructive, needInbox, &mcp.Tool{
		Name:        name,
		Description: "Delete " + what + ". Needs confirm=true.",
	}, func(ctx context.Context, in confirmIDInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := pathID(base, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, p, nil, nil)
	})
}

func crudGet(r *registry, name, what, base string) {
	add(r, read, needInbox, &mcp.Tool{
		Name:        name,
		Description: "Get " + what + ".",
	}, func(ctx context.Context, in idInput) (any, error) {
		p, err := pathID(base, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p, nil, nil)
	})
}

func registerLabelsAndReplies(r *registry) {
	const labels = "/api/v1/conversation-labels"
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_conversation_labels",
		Description: "The labels agents and automations can put on conversations.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, labels, nil, nil)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "create_conversation_label",
		Description: "Create a conversation label.",
	}, func(ctx context.Context, in labelFields) (any, error) {
		return r.api.Do(ctx, http.MethodPost, labels, nil, in)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "update_conversation_label",
		Description: "Rename a label or change its colour or description. Every field is replaced.",
	}, func(ctx context.Context, in updateLabelInput) (any, error) {
		p, err := pathID(labels, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, in.labelFields)
	})
	crudDelete(r, "delete_conversation_label", "a conversation label; it is removed from every conversation that has it", labels)

	const canned = "/api/v1/canned-responses"
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_saved_replies",
		Description: "The workspace's saved replies (canned responses) agents insert with /shortcode.",
	}, func(ctx context.Context, in searchCannedInput) (any, error) {
		var q url.Values
		if in.Search != "" {
			q = url.Values{"search": {in.Search}}
		}
		return r.api.Do(ctx, http.MethodGet, canned, q, nil)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "create_saved_reply",
		Description: "Create a saved reply.",
	}, func(ctx context.Context, in cannedFields) (any, error) {
		return r.api.Do(ctx, http.MethodPost, canned, nil, in)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "update_saved_reply",
		Description: "Change a saved reply's shortcode or text.",
	}, func(ctx context.Context, in updateCannedInput) (any, error) {
		p, err := pathID(canned, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, in.cannedFields)
	})
	crudDelete(r, "delete_saved_reply", "a saved reply", canned)
}

func registerTeamsAndRouting(r *registry) {
	const teams = "/api/v1/teams"
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_teams",
		Description: "The workspace's agent teams.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, teams, nil, nil)
	})
	crudGet(r, "get_team", "one team", teams)
	add(r, write, needInbox, &mcp.Tool{
		Name:        "create_team",
		Description: "Create an agent team. Add people with add_team_member.",
	}, func(ctx context.Context, in teamFields) (any, error) {
		return r.api.Do(ctx, http.MethodPost, teams, nil, in)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "update_team",
		Description: "Change a team's name, description or auto-assignment. Every field is replaced.",
	}, func(ctx context.Context, in updateTeamInput) (any, error) {
		p, err := pathID(teams, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, in.teamFields)
	})
	crudDelete(r, "delete_team", "a team; its conversations are left without a team", teams)
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_team_members",
		Description: "The members of one team.",
	}, func(ctx context.Context, in idInput) (any, error) {
		p, err := pathID(teams, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p+"/members", nil, nil)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "add_team_member",
		Description: "Add a workspace member to a team.",
	}, func(ctx context.Context, in memberInput) (any, error) {
		p, err := pathID(teams, "id", in.ID)
		if err != nil {
			return nil, err
		}
		uid, err := id("userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p+"/members", nil, map[string]string{"userId": uid})
	})
	add(r, destructive, needInbox, &mcp.Tool{
		Name:        "remove_team_member",
		Description: "Take a member out of a team. Needs confirm=true.",
	}, func(ctx context.Context, in memberInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := pathID(teams, "id", in.ID)
		if err != nil {
			return nil, err
		}
		p, err = pathID(p+"/members", "userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, p, nil, nil)
	})

	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_assignable_members",
		Description: "Workspace members who can be put in teams and inboxes or have conversations assigned, with their ids.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/inbox-settings/members", nil, nil)
	})
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_inbox_members",
		Description: "The agents who receive conversations from one inbox (a widget or a messaging channel).",
	}, func(ctx context.Context, in inboxInput) (any, error) {
		p, err := inboxPath(in.InboxType, in.InboxID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p+"/members", nil, nil)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "add_inbox_member",
		Description: "Let an agent receive conversations from an inbox.",
	}, func(ctx context.Context, in inboxMemberInput) (any, error) {
		p, err := inboxPath(in.InboxType, in.InboxID)
		if err != nil {
			return nil, err
		}
		uid, err := id("userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p+"/members", nil, map[string]string{"userId": uid})
	})
	add(r, destructive, needInbox, &mcp.Tool{
		Name:        "remove_inbox_member",
		Description: "Stop an agent receiving conversations from an inbox. Needs confirm=true.",
	}, func(ctx context.Context, in inboxMemberInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := inboxPath(in.InboxType, in.InboxID)
		if err != nil {
			return nil, err
		}
		p, err = pathID(p+"/members", "userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, p, nil, nil)
	})
	add(r, read, needInboxOrWorkforce, &mcp.Tool{
		Name:        "get_assignment_settings",
		Description: "How new conversations are assigned to agents.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/assignment-settings", nil, nil)
	})
	add(r, write, needInboxOrWorkforce, &mcp.Tool{
		Name:        "update_assignment_settings",
		Description: "Choose how new conversations are assigned: legacy, round_robin or balanced (fewest open conversations). Every field is replaced.",
	}, func(ctx context.Context, in assignmentSettingsInput) (any, error) {
		return r.api.Do(ctx, http.MethodPut, "/api/v1/assignment-settings", nil, in)
	})
}

func registerHoursAndSla(r *registry) {
	add(r, read, needInbox, &mcp.Tool{
		Name:        "get_business_hours",
		Description: "Opening hours of the workspace, or of one inbox when inboxType and inboxId are given (an inbox's own hours win over the workspace's).",
	}, func(ctx context.Context, in optionalInboxInput) (any, error) {
		p, err := in.path()
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p, nil, nil)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "set_business_hours",
		Description: "Set the opening hours of the workspace, or of one inbox when inboxType and inboxId are given. Every field is replaced.",
	}, func(ctx context.Context, in setBusinessHoursInput) (any, error) {
		p, err := in.path()
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, in.businessHoursFields)
	})
	add(r, destructive, needInbox, &mcp.Tool{
		Name:        "clear_inbox_business_hours",
		Description: "Remove an inbox's own opening hours so it follows the workspace's again. Needs confirm=true.",
	}, func(ctx context.Context, in clearInboxHoursInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := inboxPath(in.InboxType, in.InboxID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, p+"/business-hours", nil, nil)
	})

	const sla = "/api/v1/sla-policies"
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_sla_policies",
		Description: "Response- and resolution-time targets (SLA policies).",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, sla, nil, nil)
	})
	crudGet(r, "get_sla_policy", "one SLA policy", sla)
	add(r, write, needInbox, &mcp.Tool{
		Name:        "create_sla_policy",
		Description: "Create an SLA policy: reply and resolution targets for matching conversations.",
	}, func(ctx context.Context, in slaFields) (any, error) {
		if err := in.validate(); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, sla, nil, in)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "update_sla_policy",
		Description: "Replace an SLA policy. Every field is overwritten: read it with get_sla_policy first and send it back changed.",
	}, func(ctx context.Context, in updateSlaInput) (any, error) {
		p, err := pathID(sla, "id", in.ID)
		if err != nil {
			return nil, err
		}
		if err := in.validate(); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, in.slaFields)
	})
	crudDelete(r, "delete_sla_policy", "an SLA policy", sla)
}

func registerAutomations(r *registry) {
	const base = "/api/v1/automations"
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_automations",
		Description: "Automation rules, in the order they run: when a trigger happens and the conditions hold, run the actions.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, base, nil, nil)
	})
	crudGet(r, "get_automation", "one automation rule", base)
	add(r, write, needInbox, &mcp.Tool{
		Name: "create_automation",
		Description: "Create an automation rule. A send_canned_reply action messages customers, so show the user the rule first; " +
			"set dryRun=true to try a new rule without changing anything, then check list_automation_runs.",
	}, func(ctx context.Context, in automationFields) (any, error) {
		if err := in.validate(); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, base, nil, in)
	})
	add(r, write, needInbox, &mcp.Tool{
		Name:        "update_automation",
		Description: "Replace an automation rule. Every field is overwritten: read it with get_automation first and send it back changed.",
	}, func(ctx context.Context, in updateAutomationInput) (any, error) {
		p, err := pathID(base, "id", in.ID)
		if err != nil {
			return nil, err
		}
		if err := in.validate(); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, in.automationFields)
	})
	crudDelete(r, "delete_automation", "an automation rule", base)
	add(r, write, needInbox, &mcp.Tool{
		Name:        "reorder_automations",
		Description: "Set the order rules run in. Pass every rule id of the workspace.",
	}, func(ctx context.Context, in reorderInput) (any, error) {
		for _, v := range in.IDs {
			if _, err := id("ids", v); err != nil {
				return nil, err
			}
		}
		return r.api.Do(ctx, http.MethodPost, base+"/reorder", nil, map[string]any{"ids": in.IDs})
	})
	add(r, read, needInbox, &mcp.Tool{
		Name:        "test_automation",
		Description: "Check whether a rule's conditions match one conversation. Nothing is run or changed.",
	}, func(ctx context.Context, in testAutomationInput) (any, error) {
		p, err := pathID(base, "id", in.ID)
		if err != nil {
			return nil, err
		}
		cid, err := id("conversationId", in.ConversationID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p+"/test", nil, map[string]string{"conversationId": cid})
	})
	add(r, read, needInbox, &mcp.Tool{
		Name:        "list_automation_runs",
		Description: "What automation rules did (or would have done, for dry runs), newest first.",
	}, func(ctx context.Context, in automationRunsInput) (any, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		q := url.Values{"limit": {strconv.Itoa(limit)}}
		for k, v := range map[string]string{"ruleId": in.RuleID, "conversationId": in.ConversationID} {
			if v == "" {
				continue
			}
			if _, err := id(k, v); err != nil {
				return nil, err
			}
			q.Set(k, v)
		}
		if in.Before != "" {
			q.Set("before", in.Before)
		}
		return r.api.Do(ctx, http.MethodGet, base+"/runs", q, nil)
	})
}
