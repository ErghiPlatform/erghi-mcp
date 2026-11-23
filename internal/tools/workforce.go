package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Workforce (manage_workforce): agent status, away reasons, routing skills and
// per-agent routing profiles, the workforce settings, and the time-in-status
// reports (view_insights).
//
// A key with manage_workforce can read what it can change (the live board, the
// away reasons, the skills and the settings). "My status" and a conversation's
// routing decision need a signed-in teammate or view_conversations, which a key
// never holds, so they are not wrapped here.

const (
	scopeWorkforce = "manage_workforce"
	scopeSocial    = "moderate_social"
)

var (
	needWorkforce        = []string{scopeWorkforce}
	needInboxOrWorkforce = []string{scopeInbox, scopeWorkforce}
)

// callFeature sends a request to an endpoint that only newer Erghi servers have.
// A 404 with no body is what an unknown route answers; the API's own "not
// found" answers carry a message or a code, so they pass through unchanged.
func (r *registry) callFeature(ctx context.Context, feature, method, path string, q url.Values, body any) (any, error) {
	raw, err := r.api.Do(ctx, method, path, q, body)
	if err == nil {
		return raw, nil
	}
	var apiErr *erghi.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound && apiErr.Code == "" &&
		(apiErr.Message == "" || strings.EqualFold(apiErr.Message, "not found")) {
		return nil, fmt.Errorf("this Erghi server doesn't support %s yet", feature)
	}
	return nil, err
}

const workforceBase = "/api/v1/workforce"

type agentInput struct {
	UserID string `json:"userId" jsonschema:"The teammate (a workspace member id, from list_assignable_members)"`
}

type setAgentStatusInput struct {
	agentInput
	State       string  `json:"state" jsonschema:"ready, away or offline"`
	ReasonID    *string `json:"reasonId,omitempty" jsonschema:"Away only: an away reason id"`
	Note        *string `json:"note,omitempty" jsonschema:"Away only: a short supervisor note. Stored, but never shown to an API key."`
	ReturnChats bool    `json:"returnChats,omitempty" jsonschema:"Away only: put the agent's assigned chats back in the queue now"`
	Confirm     bool    `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed to change this teammate's status."`
}

type signOutAgentInput struct {
	agentInput
	Confirm bool `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed to sign this teammate out."`
}

type awayReasonFields struct {
	Name          string  `json:"name" jsonschema:"At most 40 characters; unique in the workspace"`
	Color         *string `json:"color,omitempty" jsonschema:"Hex colour, e.g. #2563eb"`
	MaxMinutes    *int    `json:"maxMinutes,omitempty" jsonschema:"1 to 1440: how long this reason is expected to last; longer shows as over the limit"`
	ReassignChats bool    `json:"reassignChats" jsonschema:"Going away with this reason puts the agent's chats back in the queue. Always sent, so say it explicitly."`
	SortOrder     *int    `json:"sortOrder,omitempty"`
}

type updateAwayReasonInput struct {
	ID string `json:"id"`
	awayReasonFields
	Archived *bool `json:"archived,omitempty" jsonschema:"false restores an archived reason"`
}

type skillFields struct {
	Name        string `json:"name" jsonschema:"The skill's name, unique among active skills"`
	Description string `json:"description" jsonschema:"What the skill covers. The AI reads this to match chats to it, so describe the topics concretely."`
}

type updateSkillInput struct {
	ID string `json:"id"`
	skillFields
	Archived *bool `json:"archived,omitempty" jsonschema:"false restores an archived skill, true archives it"`
}

type agentSkillLevel struct {
	SkillID string `json:"skillId" jsonschema:"An active routing skill id"`
	Level   int    `json:"level" jsonschema:"1 basic, 2 skilled, 3 expert"`
}

type updateAgentProfileInput struct {
	agentInput
	MaxConcurrentChats *int              `json:"maxConcurrentChats,omitempty" jsonschema:"At most this many open chats at once; 0 means never auto-assign; leave out for the workspace default"`
	Languages          []string          `json:"languages,omitempty" jsonschema:"Language codes the agent handles, e.g. ar, en. Always replaced: send the full list, empty clears it."`
	Skills             []agentSkillLevel `json:"skills,omitempty" jsonschema:"The agent's skills. When present it replaces the agent's skill list (an empty list clears it); leave it out to keep them."`
}

type routingSettingsFields struct {
	AIRoutingEnabled    *bool   `json:"aiRoutingEnabled,omitempty" jsonschema:"Let the AI classify each chat (skills, language) before choosing an agent; needs a plan that includes it"`
	LanguageMatch       *string `json:"languageMatch,omitempty" jsonschema:"off, preferred or required: how much the chat's language decides who gets it"`
	SkillWaitSeconds    *int    `json:"skillWaitSeconds,omitempty" jsonschema:"How long a chat waits for a matching skilled agent before anyone may take it"`
	OfferMode           *string `json:"offerMode,omitempty" jsonschema:"off assigns directly; ring offers the chat to one agent at a time, who accepts or declines"`
	OfferTimeoutSeconds *int    `json:"offerTimeoutSeconds,omitempty" jsonschema:"With offerMode=ring: how long an agent has to answer an offer before it goes to the next"`
}

type workforceSettingsInput struct {
	StatusOnSignIn            string                 `json:"statusOnSignIn,omitempty" jsonschema:"ready or away: the status an agent gets when they first connect; leave out to keep"`
	RequireAwayReason         *bool                  `json:"requireAwayReason,omitempty" jsonschema:"Agents must pick a reason when they go away; leave out to keep"`
	IdleAwayMinutes           *int                   `json:"idleAwayMinutes,omitempty" jsonschema:"Move a Ready agent with no activity for this many minutes to Away (reads agent activity); 0 turns it off, leave out to keep"`
	SignOutAfterHoursMinutes  *int                   `json:"signOutAfterHoursMinutes,omitempty" jsonschema:"Sign agents out this many minutes after business hours end; 0 turns it off, leave out to keep"`
	AutoAwayAfterMissedOffers *int                   `json:"autoAwayAfterMissedOffers,omitempty" jsonschema:"Move an agent to Away after this many missed offers in a row; 0 turns it off, leave out to keep"`
	Routing                   *routingSettingsFields `json:"routing,omitempty" jsonschema:"Routing options; leave a field out to keep it"`
}

type includeArchivedInput struct {
	IncludeArchived bool `json:"includeArchived,omitempty" jsonschema:"Also list archived ones"`
}

func (f includeArchivedInput) query() url.Values {
	if !f.IncludeArchived {
		return nil
	}
	return url.Values{"includeArchived": {"true"}}
}

type workforceReportInput struct {
	From     string `json:"from,omitempty" jsonschema:"Start date, YYYY-MM-DD (default: recent days); at most 92 days from to"`
	To       string `json:"to,omitempty" jsonschema:"End date, YYYY-MM-DD"`
	TimeZone string `json:"timeZone,omitempty" jsonschema:"IANA time zone the days are counted in, e.g. Africa/Cairo"`
	GroupBy  string `json:"groupBy,omitempty" jsonschema:"agent (default), team, day or agent_day"`
}

type agentTimelineInput struct {
	AgentID  string `json:"agentId" jsonschema:"The teammate to show"`
	From     string `json:"from,omitempty" jsonschema:"Start date, YYYY-MM-DD"`
	To       string `json:"to,omitempty" jsonschema:"End date, YYYY-MM-DD"`
	TimeZone string `json:"timeZone,omitempty" jsonschema:"IANA time zone, e.g. Africa/Cairo"`
}

func (f workforceReportInput) query(withGroup bool) url.Values {
	q := url.Values{}
	for k, v := range map[string]string{"from": f.From, "to": f.To, "timeZone": f.TimeZone} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if withGroup && f.GroupBy != "" {
		q.Set("groupBy", f.GroupBy)
	}
	return q
}

const unsupportedWorkforce = "workforce management"

func registerWorkforce(r *registry) {
	registerAgentControl(r)
	registerAwayReasonsAndSkills(r)
	registerAgentProfilesAndSettings(r)
	registerWorkforceReports(r)
}

func registerAgentControl(r *registry) {
	add(r, write, needWorkforce, &mcp.Tool{
		Name: "set_agent_status",
		Description: "Force a teammate's status to ready, away (optionally with a reason) or offline, as a supervisor. Needs confirm=true. " +
			"Away with returnChats=true puts their chats back in the queue immediately. The change is recorded in the audit log.",
	}, func(ctx context.Context, in setAgentStatusInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := pathID(workforceBase+"/agents", "userId", in.UserID)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"state": in.State, "returnChats": in.ReturnChats}
		if in.ReasonID != nil {
			if _, err := id("reasonId", *in.ReasonID); err != nil {
				return nil, err
			}
			body["reasonId"] = *in.ReasonID
		}
		if in.Note != nil {
			body["note"] = *in.Note
		}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPut, p+"/status", nil, body)
	})
	add(r, destructive, needWorkforce, &mcp.Tool{
		Name: "sign_out_agent",
		Description: "Sign a teammate out (status offline) and return their assigned chats to the queue. Needs confirm=true. " +
			"They can sign back in themselves.",
	}, func(ctx context.Context, in signOutAgentInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := pathID(workforceBase+"/agents", "userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPost, p+"/sign-out", nil, nil)
	})
}

func registerAwayReasonsAndSkills(r *registry) {
	const reasons = workforceBase + "/away-reasons"
	add(r, read, needWorkforce, &mcp.Tool{
		Name:        "list_away_reasons",
		Description: "The away reasons agents can pick (system ones like Lunch and Break, and the workspace's own), with colour, time limit, order and whether going away returns chats to the queue.",
	}, func(ctx context.Context, in includeArchivedInput) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodGet, reasons, in.query(), nil)
	})
	add(r, read, needWorkforce, &mcp.Tool{
		Name:        "list_routing_skills",
		Description: "The routing skills (name and description). With AI routing on, the AI matches chats to skills by these descriptions.",
	}, func(ctx context.Context, in includeArchivedInput) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodGet, workforceBase+"/skills", in.query(), nil)
	})
	add(r, write, needWorkforce, &mcp.Tool{
		Name: "create_away_reason",
		Description: "Add an away reason agents can choose (Lunch, Training, ...). The answer is the new reason with its id. " +
			"Check list_away_reasons first so you don't add a duplicate.",
	}, func(ctx context.Context, in awayReasonFields) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPost, reasons, nil, in)
	})
	add(r, write, needWorkforce, &mcp.Tool{
		Name:        "update_away_reason",
		Description: "Change an away reason's name, colour, limit, order or whether it returns chats to the queue, or restore an archived one (archived=false).",
	}, func(ctx context.Context, in updateAwayReasonInput) (any, error) {
		p, err := pathID(reasons, "id", in.ID)
		if err != nil {
			return nil, err
		}
		body := struct {
			awayReasonFields
			Archived *bool `json:"archived,omitempty"`
		}{in.awayReasonFields, in.Archived}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPut, p, nil, body)
	})
	add(r, destructive, needWorkforce, &mcp.Tool{
		Name: "archive_away_reason",
		Description: "Archive an away reason: agents can no longer pick it, past history keeps it. Restore it with update_away_reason (archived=false). " +
			"The default Away reason can't be archived. Needs confirm=true.",
	}, func(ctx context.Context, in confirmIDInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := pathID(reasons, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodDelete, p, nil, nil)
	})

	const skills = workforceBase + "/skills"
	add(r, write, needWorkforce, &mcp.Tool{
		Name: "create_routing_skill",
		Description: "Add a routing skill (e.g. Billing, Arabic support). With AI routing on, the AI matches each chat to skills by their descriptions, so describe the topics concretely. " +
			"The answer is the new skill with its id.",
	}, func(ctx context.Context, in skillFields) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPost, skills, nil, in)
	})
	add(r, write, needWorkforce, &mcp.Tool{
		Name:        "update_routing_skill",
		Description: "Rename a routing skill, change its description, or archive (archived=true) or restore (archived=false) it.",
	}, func(ctx context.Context, in updateSkillInput) (any, error) {
		p, err := pathID(skills, "id", in.ID)
		if err != nil {
			return nil, err
		}
		body := struct {
			skillFields
			Archived *bool `json:"archived,omitempty"`
		}{in.skillFields, in.Archived}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPut, p, nil, body)
	})
	add(r, destructive, needWorkforce, &mcp.Tool{
		Name: "archive_routing_skill",
		Description: "Archive a routing skill: it stops matching new chats; agent links and past routing decisions keep resolving. " +
			"Restore it with update_routing_skill (archived=false). Needs confirm=true.",
	}, func(ctx context.Context, in confirmIDInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := pathID(skills, "id", in.ID)
		if err != nil {
			return nil, err
		}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodDelete, p, nil, nil)
	})
}

func registerAgentProfilesAndSettings(r *registry) {
	add(r, read, needWorkforce, &mcp.Tool{
		Name:        "get_workforce_settings",
		Description: "The workforce settings: status on sign-in, required away reason, idle-away, sign-out after hours, auto-away after missed offers (null means off), and the routing options.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodGet, workforceBase+"/settings", nil, nil)
	})
	add(r, read, needWorkforce, &mcp.Tool{
		Name: "get_workforce_live",
		Description: "The live board: each teammate's current status (ready, busy, away with reason, offline) and since when, their open chats against capacity, and the queue. " +
			"Supervisors' notes on away reasons are never included for an API key.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodGet, workforceBase+"/live", nil, nil)
	})
	add(r, read, needWorkforce, &mcp.Tool{
		Name:        "get_agent_routing_profile",
		Description: "One teammate's routing profile: their chat capacity, languages and skills with levels.",
	}, func(ctx context.Context, in agentInput) (any, error) {
		p, err := pathID(workforceBase+"/agents", "userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodGet, p+"/profile", nil, nil)
	})
	add(r, write, needWorkforce, &mcp.Tool{
		Name: "update_agent_routing_profile",
		Description: "Set a teammate's chat capacity, languages and skills, which decide who auto-assignment picks. Languages are replaced; skills are replaced only when sent. " +
			"Read get_agent_routing_profile first and send back what should stay.",
	}, func(ctx context.Context, in updateAgentProfileInput) (any, error) {
		p, err := pathID(workforceBase+"/agents", "userId", in.UserID)
		if err != nil {
			return nil, err
		}
		for i, s := range in.Skills {
			if _, err := id(fmt.Sprintf("skills[%d].skillId", i), s.SkillID); err != nil {
				return nil, err
			}
		}
		body := map[string]any{"languages": in.Languages}
		if in.Languages == nil {
			body["languages"] = []string{}
		}
		if in.MaxConcurrentChats != nil {
			body["maxConcurrentChats"] = *in.MaxConcurrentChats
		}
		if in.Skills != nil {
			body["skills"] = in.Skills
		}
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPut, p+"/profile", nil, body)
	})
	add(r, write, needWorkforce, &mcp.Tool{
		Name: "update_workforce_settings",
		Description: "Workforce settings: the status agents get on sign-in, whether an away reason is required, idle-away, sign-out after business hours, " +
			"auto-away after missed offers, and the routing options (AI routing, language match, skill wait, offer mode ring and its timeout). " +
			"Send only what changes: every field you leave out keeps its value. Send 0 to turn idle-away, sign-out after hours or auto-away off. " +
			"Read get_workforce_settings first.",
	}, func(ctx context.Context, in workforceSettingsInput) (any, error) {
		return r.callFeature(ctx, unsupportedWorkforce, http.MethodPut, workforceBase+"/settings", nil, in)
	})
}

func registerWorkforceReports(r *registry) {
	add(r, read, needInsights, &mcp.Tool{
		Name: "get_agent_status_report",
		Description: "Time each agent (or team, or day) spent ready, away and offline over a date range, with the away reasons used. " +
			"Needs a plan with workforce analytics.",
	}, func(ctx context.Context, in workforceReportInput) (any, error) {
		return r.callFeature(ctx, "the workforce reports", http.MethodGet, "/api/v1/reports/agent-status", in.query(true), nil)
	})
	add(r, read, needInsights, &mcp.Tool{
		Name: "get_agent_status_timeline",
		Description: "One teammate's status changes over a date range: each interval's state, away reason, source and time over the reason's limit. " +
			"Supervisors' notes are never included for an API key.",
	}, func(ctx context.Context, in agentTimelineInput) (any, error) {
		aid, err := id("agentId", in.AgentID)
		if err != nil {
			return nil, err
		}
		q := workforceReportInput{From: in.From, To: in.To, TimeZone: in.TimeZone}.query(false)
		q.Set("agentId", aid)
		return r.callFeature(ctx, "the workforce reports", http.MethodGet, "/api/v1/reports/agent-status/timeline", q, nil)
	})
	add(r, read, needInsights, &mcp.Tool{
		Name: "get_agent_performance_report",
		Description: "Workload and quality per agent, team or day: time in each state, chats handled, utilisation against capacity, and offer metrics " +
			"(received, accepted, missed, accept time).",
	}, func(ctx context.Context, in workforceReportInput) (any, error) {
		return r.callFeature(ctx, "the workforce reports", http.MethodGet, "/api/v1/reports/agent-performance", in.query(true), nil)
	})
}
