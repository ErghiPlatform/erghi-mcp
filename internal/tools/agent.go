package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Agent work on conversations (chat:read, chat:write) and contacts
// (contacts:read, contacts:write), through /api/v1/agent/*.
//
// A key acts as itself, never as a workspace member: a note it adds shows the
// key's name followed by "(API key)", and the audit log records the key's
// client id. Only shared macros are visible to a key.

const agentConversations = "/api/v1/agent/conversations"

var (
	priorities     = []string{"none", "low", "medium", "high", "urgent"}
	bulkActions    = []string{"resolve", "reopen", "assign", "unassign", "add_label", "remove_label", "set_priority", "snooze", "set_team"}
	copilotActions = []string{"draft", "summarize", "rewrite", "translate"}
	copilotTones   = []string{"friendly", "formal", "shorter", "clearer"}
)

// maxBulk is the API's limit on conversations per bulk call.
const maxBulk = 100

func oneOf(name, v string, allowed []string) error {
	if !slices.Contains(allowed, v) {
		return fmt.Errorf("%s must be one of %v, got %q", name, allowed, v)
	}
	return nil
}

// optionalID validates an id that may be left out.
func optionalID(name string, v *string) error {
	if v == nil {
		return nil
	}
	_, err := id(name, *v)
	return err
}

func conversationPath(conversationID string, rest ...string) (string, error) {
	p, err := pathID(agentConversations, "conversationId", conversationID)
	if err != nil {
		return "", err
	}
	for _, s := range rest {
		p += "/" + s
	}
	return p, nil
}

type conversationInput struct {
	ConversationID string `json:"conversationId"`
}

type addNoteInput struct {
	ConversationID   string   `json:"conversationId"`
	Content          string   `json:"content" jsonschema:"The internal note. Customers never see notes."`
	MentionedUserIDs []string `json:"mentionedUserIds,omitempty" jsonschema:"Members to notify, from get_agent_inbox_options"`
}

type conversationLabelInput struct {
	ConversationID string `json:"conversationId"`
	LabelID        string `json:"labelId" jsonschema:"A label from get_agent_inbox_options"`
}

type snoozeInput struct {
	ConversationID string  `json:"conversationId"`
	SnoozeMinutes  *int    `json:"snoozeMinutes,omitempty" jsonschema:"Snooze for this many minutes"`
	Until          *string `json:"until,omitempty" jsonschema:"Or until this time, ISO 8601"`
	UntilNextReply bool    `json:"untilNextReply,omitempty" jsonschema:"Or until the customer writes again"`
}

type priorityInput struct {
	ConversationID string `json:"conversationId"`
	Priority       string `json:"priority" jsonschema:"none, low, medium, high or urgent"`
}

type teamInput struct {
	ConversationID string  `json:"conversationId"`
	TeamID         *string `json:"teamId" jsonschema:"A team from get_agent_inbox_options; null removes the team"`
}

type assignInput struct {
	ConversationID string `json:"conversationId"`
	UserID         string `json:"userId" jsonschema:"An active member from get_agent_inbox_options"`
}

type bulkPayload struct {
	LabelID        *string `json:"labelId,omitempty" jsonschema:"add_label, remove_label"`
	UserID         *string `json:"userId,omitempty" jsonschema:"assign"`
	Priority       *string `json:"priority,omitempty" jsonschema:"set_priority"`
	SnoozeMinutes  *int    `json:"snoozeMinutes,omitempty" jsonschema:"snooze"`
	Until          *string `json:"until,omitempty" jsonschema:"snooze, ISO 8601"`
	UntilNextReply bool    `json:"untilNextReply,omitempty" jsonschema:"snooze"`
	TeamID         *string `json:"teamId,omitempty" jsonschema:"set_team; leave out to remove the team"`
}

type bulkInput struct {
	ConversationIDs []string     `json:"conversationIds" jsonschema:"At most 100"`
	Action          string       `json:"action" jsonschema:"resolve, reopen, assign, unassign, add_label, remove_label, set_priority, snooze or set_team"`
	Payload         *bulkPayload `json:"payload,omitempty"`
	Confirm         bool         `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

type bulkBody struct {
	ConversationIDs []string     `json:"conversationIds"`
	Action          string       `json:"action"`
	Payload         *bulkPayload `json:"payload,omitempty"`
}

func (b bulkInput) validate() error {
	if len(b.ConversationIDs) == 0 || len(b.ConversationIDs) > maxBulk {
		return fmt.Errorf("conversationIds must hold 1 to %d ids", maxBulk)
	}
	for i, c := range b.ConversationIDs {
		if _, err := id(fmt.Sprintf("conversationIds[%d]", i), c); err != nil {
			return err
		}
	}
	if err := oneOf("action", b.Action, bulkActions); err != nil {
		return err
	}
	if p := b.Payload; p != nil {
		for name, v := range map[string]*string{"payload.labelId": p.LabelID, "payload.userId": p.UserID, "payload.teamId": p.TeamID} {
			if err := optionalID(name, v); err != nil {
				return err
			}
		}
		if p.Priority != nil {
			if err := oneOf("payload.priority", *p.Priority, priorities); err != nil {
				return err
			}
		}
	}
	return nil
}

type applyMacroInput struct {
	ConversationID string `json:"conversationId"`
	MacroID        string `json:"macroId" jsonschema:"A shared macro from get_agent_inbox_options"`
	Confirm        bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has agreed: a macro can send a saved reply to the customer."`
}

type copilotInput struct {
	ConversationID string  `json:"conversationId"`
	Action         string  `json:"action" jsonschema:"draft (a reply), summarize (the conversation), rewrite (text in a tone) or translate (text or a message into targetLanguage)"`
	Text           *string `json:"text,omitempty" jsonschema:"rewrite and translate: the text"`
	Tone           *string `json:"tone,omitempty" jsonschema:"rewrite: friendly, formal, shorter or clearer"`
	TargetLanguage *string `json:"targetLanguage,omitempty" jsonschema:"translate: e.g. ar, en, es"`
	MessageID      *string `json:"messageId,omitempty" jsonschema:"translate: a message of this conversation, instead of text"`
}

type copilotBody struct {
	Action         string  `json:"action"`
	Text           *string `json:"text,omitempty"`
	Tone           *string `json:"tone,omitempty"`
	TargetLanguage *string `json:"targetLanguage,omitempty"`
	MessageID      *string `json:"messageId,omitempty"`
}

func registerAgentWork(r *registry) {
	add(r, read, needChatRead, &mcp.Tool{
		Name: "get_agent_inbox_options",
		Description: "The ids to work conversations with: the workspace's labels, teams, members a conversation can be assigned to, " +
			"and shared macros with their actions.",
	}, func(ctx context.Context, _ empty) (any, error) {
		return r.api.Do(ctx, http.MethodGet, "/api/v1/agent/inbox", nil, nil)
	})
	add(r, read, needChatRead, &mcp.Tool{
		Name:        "list_conversation_notes",
		Description: "Internal notes on a conversation, oldest first. Teammates write them; customers never see them.",
	}, func(ctx context.Context, in conversationInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "notes")
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p, nil, nil)
	})
	add(r, read, needChatRead, &mcp.Tool{
		Name:        "list_labels_on_conversation",
		Description: "The labels on one conversation.",
	}, func(ctx context.Context, in conversationInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "labels")
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p, nil, nil)
	})

	add(r, write, needChatWrite, &mcp.Tool{
		Name: "add_conversation_note",
		Description: "Add an internal note for teammates (the customer never sees it). " +
			"It shows the API key's name as the author, and notes added by a key can't be edited or deleted through it.",
	}, func(ctx context.Context, in addNoteInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "notes")
		if err != nil {
			return nil, err
		}
		for i, u := range in.MentionedUserIDs {
			if _, err := id(fmt.Sprintf("mentionedUserIds[%d]", i), u); err != nil {
				return nil, err
			}
		}
		body := map[string]any{"content": in.Content}
		if len(in.MentionedUserIDs) > 0 {
			body["mentionedUserIds"] = in.MentionedUserIDs
		}
		return r.api.Do(ctx, http.MethodPost, p, nil, body)
	})
	for _, t := range []struct {
		name, desc, method string
	}{
		{"add_label_to_conversation", "Put a label on a conversation. Returns the conversation's labels.", http.MethodPost},
		{"remove_label_from_conversation", "Take a label off a conversation. Returns the conversation's labels.", http.MethodDelete},
	} {
		add(r, write, needChatWrite, &mcp.Tool{Name: t.name, Description: t.desc},
			func(ctx context.Context, in conversationLabelInput) (any, error) {
				p, err := conversationPath(in.ConversationID, "labels")
				if err != nil {
					return nil, err
				}
				p, err = pathID(p, "labelId", in.LabelID)
				if err != nil {
					return nil, err
				}
				return r.api.Do(ctx, t.method, p, nil, nil)
			})
	}
	add(r, write, needChatWrite, &mcp.Tool{
		Name:        "snooze_conversation",
		Description: "Snooze a conversation: give exactly one of snoozeMinutes, until or untilNextReply. It comes back when the time passes or the customer writes.",
	}, func(ctx context.Context, in snoozeInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "snooze")
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p, nil, map[string]any{
			"snoozeMinutes": in.SnoozeMinutes, "until": in.Until, "untilNextReply": in.UntilNextReply,
		})
	})
	simple := []struct {
		name, desc, method, sub string
	}{
		{"unsnooze_conversation", "Wake a snoozed conversation now.", http.MethodDelete, "snooze"},
		{"unassign_conversation", "Take the conversation off its assignee; it goes back to the queue.", http.MethodPost, "unassign"},
		{"reopen_conversation", "Reopen a resolved conversation.", http.MethodPost, "reopen"},
	}
	for _, t := range simple {
		add(r, write, needChatWrite, &mcp.Tool{Name: t.name, Description: t.desc},
			func(ctx context.Context, in conversationInput) (any, error) {
				p, err := conversationPath(in.ConversationID, t.sub)
				if err != nil {
					return nil, err
				}
				return r.api.Do(ctx, t.method, p, nil, nil)
			})
	}
	add(r, write, needChatWrite, &mcp.Tool{
		Name:        "set_conversation_priority",
		Description: "Set a conversation's priority.",
	}, func(ctx context.Context, in priorityInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "priority")
		if err != nil {
			return nil, err
		}
		if err := oneOf("priority", in.Priority, priorities); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, map[string]any{"priority": in.Priority})
	})
	add(r, write, needChatWrite, &mcp.Tool{
		Name:        "set_conversation_team",
		Description: "Route a conversation to a team, or remove its team with teamId null.",
	}, func(ctx context.Context, in teamInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "team")
		if err != nil {
			return nil, err
		}
		if err := optionalID("teamId", in.TeamID); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPut, p, nil, map[string]any{"teamId": in.TeamID})
	})
	add(r, write, needChatWrite, &mcp.Tool{
		Name:        "assign_conversation",
		Description: "Assign a conversation to a workspace member. Refused if they are not an active member or are at their limit of open conversations.",
	}, func(ctx context.Context, in assignInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "assign")
		if err != nil {
			return nil, err
		}
		u, err := id("userId", in.UserID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p, nil, map[string]any{"agentId": u})
	})
	add(r, write, needChatWrite, &mcp.Tool{
		Name: "bulk_update_conversations",
		Description: "Apply one action to up to 100 conversations. Returns the ids that changed and, for each skipped one, why. " +
			"Needs confirm=true.",
	}, func(ctx context.Context, in bulkInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		if err := in.validate(); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, agentConversations+"/bulk", nil,
			bulkBody{ConversationIDs: in.ConversationIDs, Action: in.Action, Payload: in.Payload})
	})
	add(r, write, needChatWrite, &mcp.Tool{
		Name: "apply_macro",
		Description: "Run a shared macro on a conversation (its actions run in order; a send_canned_reply action messages the customer). " +
			"Show the user the macro's actions first. Needs confirm=true.",
	}, func(ctx context.Context, in applyMacroInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		p, err := conversationPath(in.ConversationID, "macros")
		if err != nil {
			return nil, err
		}
		p, err = pathID(p, "macroId", in.MacroID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p+"/apply", nil, nil)
	})
	add(r, write, needChatWrite, &mcp.Tool{
		Name: "run_reply_copilot",
		Description: "Draft a reply, summarize the conversation, rewrite text in a tone, or translate. Nothing is sent: " +
			"use send_reply for that, after the user approves. Each call counts as one AI reply of the workspace's plan.",
	}, func(ctx context.Context, in copilotInput) (any, error) {
		p, err := conversationPath(in.ConversationID, "copilot")
		if err != nil {
			return nil, err
		}
		if err := oneOf("action", in.Action, copilotActions); err != nil {
			return nil, err
		}
		if in.Tone != nil {
			if err := oneOf("tone", *in.Tone, copilotTones); err != nil {
				return nil, err
			}
		}
		if err := optionalID("messageId", in.MessageID); err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, p, nil, copilotBody{
			Action: in.Action, Text: in.Text, Tone: in.Tone, TargetLanguage: in.TargetLanguage, MessageID: in.MessageID,
		})
	})
}

// ── Contacts (contacts:read, contacts:write) ─────────────────────────────

const agentContacts = "/api/v1/agent/contacts"

type listContactsInput struct {
	Search   string `json:"search,omitempty" jsonschema:"Part of a name, email, phone number or external id"`
	Page     int    `json:"page,omitempty" jsonschema:"Page number, starting at 1"`
	PageSize int    `json:"pageSize,omitempty" jsonschema:"Contacts per page (default 25, max 100)"`
}

type contactInput struct {
	ContactID string `json:"contactId"`
}

type updateContactInput struct {
	ContactID string  `json:"contactId"`
	Name      *string `json:"name,omitempty" jsonschema:"Leave out to keep; empty string clears"`
	Email     *string `json:"email,omitempty" jsonschema:"Leave out to keep; empty string clears"`
	Phone     *string `json:"phone,omitempty" jsonschema:"Leave out to keep; empty string clears"`
}

func registerContacts(r *registry) {
	add(r, read, needContactsRead, &mcp.Tool{
		Name: "list_contacts",
		Description: "Search the workspace's contacts (people who have chatted), most recently seen first. " +
			"Contact details are personal data: fetch only what the task needs. Every call is audited.",
	}, func(ctx context.Context, in listContactsInput) (any, error) {
		size := in.PageSize
		if size <= 0 {
			size = 25
		}
		q := url.Values{"page": {strconv.Itoa(max(in.Page, 1))}, "pageSize": {strconv.Itoa(min(size, 100))}}
		if in.Search != "" {
			q.Set("search", in.Search)
		}
		return r.api.Do(ctx, http.MethodGet, agentContacts, q, nil)
	})
	add(r, read, needContactsRead, &mcp.Tool{
		Name:        "get_contact",
		Description: "One contact with their recent conversations. Every call is audited.",
	}, func(ctx context.Context, in contactInput) (any, error) {
		p, err := pathID(agentContacts, "contactId", in.ContactID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodGet, p, nil, nil)
	})
	add(r, write, needContactsEdit, &mcp.Tool{
		Name: "update_contact",
		Description: "Change a contact's name, email or phone. Show the user the change first. " +
			"Blocking or erasing a contact is done by people in the admin app.",
	}, func(ctx context.Context, in updateContactInput) (any, error) {
		p, err := pathID(agentContacts, "contactId", in.ContactID)
		if err != nil {
			return nil, err
		}
		if in.Name == nil && in.Email == nil && in.Phone == nil {
			return nil, fmt.Errorf("give at least one of name, email or phone")
		}
		return r.api.Do(ctx, http.MethodPatch, p, nil, map[string]any{"name": in.Name, "email": in.Email, "phone": in.Phone})
	})
}
