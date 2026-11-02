# How an Erghi assistant builds a reply

Every visitor message goes through the same steps. Each one is something you can configure, and each has a tool.

| Step | What happens | You control it with |
|---|---|---|
| 1. Persona and rules | The system prompt starts with "You are <assistant name> representing <company>", Erghi's fixed rules, a short-answer style, the tone, the language rule, then the workspace's own instructions. | `update_branding`: `aiAssistantName`, `companyName`, `aiTone`, `aiAudience`, `aiSystemPrompt`, `primaryLanguage`, `visitorContextHints` |
| 2. Visitor context | Non-secret conversation metadata and, when the host app signed the user in, the verified identity (`sub`, `name`, `email`). | The host app (SDK `identify`, `setContext`, identity token); see topic `integrations` |
| 3. Knowledge | The visitor's message is searched against the knowledge base. Matching *reference* excerpts are added for that turn. Up to 2 *instruction* documents (playbooks) that match are added as operator instructions. | `crawl_website`, `add_knowledge_document` (`kind=instruction` for playbooks), `add_faq`, `search_knowledge_base` |
| 4. Tools | Every endpoint of every active integration is offered to the model as a tool, with the integration's and endpoint's AI instructions appended to its description. Bound parameters are hidden from the model and filled by Erghi. | `add_integration_from_openapi`, `set_integration_tool_context`, `set_integration_active` |
| 5. Policy gate | Before a protected tool call runs, its action policies are checked: required earlier steps, a verified identity, argument rules. The gate fails closed. | `create_action_policy`, `list_protected_operations` |
| 6. Hand-off | The model calls `request_human_handoff` when the visitor asks for a person (always allowed) or, depending on the escalation policy, when it can't help. | `update_ai_settings` (`escalationPolicy`: Eager, Balanced, Persistent, SelfReliant), `update_branding` (`handoffMode`, `escalationKeywords`, hand-off messages) |
| 7. Model | The workspace's model answers; on failure the fallback chain is tried. A monthly spend cap stops AI replies when reached. | `update_ai_settings`, `list_available_models`, `get_ai_cost_usage` |
| 8. Channels | The same assistant answers the website widget, Telegram and WhatsApp; Slack gets hand-off notifications. | `create_widget`, `connect_telegram`, `connect_whatsapp`, `connect_slack` |

Rules the assistant always follows, whatever the instructions say:
- Facts about the business come only from the knowledge base, tool results or the workspace's instructions. A gap in knowledge shows up as "I don't know" or a hand-off, never a guess. So **fix wrong or missing answers in the knowledge base, not in the instructions**.
- Live data (orders, balances, bookings) needs a tool call before any number is stated.
- It never asks the visitor for API keys or credentials.

What goes where:
- **Instructions** (`aiSystemPrompt`): who the assistant serves, scope, things it must never do, when to hand off. Sent on every turn, and a determined visitor can get the model to reveal them, so no secrets.
- **Reference knowledge**: product facts, prices, policies, how-tos. Only the relevant excerpts are used per turn.
- **Instruction documents**: long topic playbooks ("refund dispute flow", "KYC questions"). Retrieved only on turns about that topic.
- **Integration AI instructions**: when to call which endpoint, units and formats, what to say with the result.
- **Action policies**: hard rules the model can't talk its way around.

Check your work with `test_assistant` (sandbox, nothing reaches visitors) and `review_chat_setup`.
