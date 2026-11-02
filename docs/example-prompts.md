# Example prompts

Copy any of these into your MCP client. Replace the bracketed parts. If your client supports MCP prompts, the built-in `setup_erghi`, `configure_ai_assistant`, `build_knowledge_base`, `brand_widget` and `install_widget` prompts do the same with more detail.

The model can only do what your API key's scopes allow. It checks them first with `get_api_key_info` and tells you if a step needs a scope the key doesn't have.

## Set up a new workspace (the dashboard checklist)

Use this right after you sign up. It follows the **Get your assistant live** checklist on the Erghi dashboard.

Key scopes: `configure_ai`, `manage_workspace_identity`, `manage_widgets`, `chat:read`.

```text
Set up Erghi for [Company] (website: [https://example.com], main language: [en]) so the
"Get your assistant live" checklist on the dashboard is complete.

Start with get_api_key_info and get_setup_checklist and show me the checklist. Then work through
the steps that aren't done, checking with me before each change:

1. Assistant: read get_branding, propose an assistant name, tone and short instructions (who it
   helps, what it shouldn't do, when to hand off to a person), plus brand colours and
   support/privacy/terms links. Apply with update_branding once I agree.
2. Knowledge: crawl_website on the site and wait for get_crawl_job to finish. Ask me about
   pricing, shipping, refunds and opening hours if the site doesn't cover them and add them with
   add_faq. Test with three test_assistant questions, one in the main language.
3. Widget: create_widget limited to my domain, with a welcome message in the main language. Give
   me get_widget_embed_code and tell me where to paste it.
4. Teammate: tell me where to invite someone in the admin app (keys can't do this).
5. First conversation: once I've sent a message through the widget, run get_setup_checklist again
   and show me the conversation with get_conversation.

Finish with get_setup_checklist and a short summary of what's done and what's left.
```

## Check setup progress

Any key works for this. Steps the key's scopes can't see come back as `unknown`.

```text
Show me my Erghi setup checklist and tell me the next thing to do.
```

## Improve answers

Key scopes: `configure_ai`.

```text
Ask the Erghi assistant five questions a real customer of [Company] would ask, using
test_assistant. Show me the answers, tell me which ones are wrong or vague, and fix the gaps by
adding FAQs or knowledge documents (not by making the instructions longer). Re-test afterwards.
```

## Keep the knowledge base current

Key scopes: `configure_ai`.

```text
Schedule a weekly re-crawl of [https://example.com/help] with schedule_website_crawl, then list
my crawl schedules to confirm it.
```

## Brand the widget

Key scopes: `manage_workspace_identity`, `manage_widgets`.

```text
Match the Erghi widget to my brand: primary colour [#0F766E], dark theme, bottom-left. Write the
welcome, hand-off and offline messages in [Arabic and English]. Show me the changes before
applying them.
```

## Answer open conversations

Key scopes: `chat:read`, `chat:write`.

```text
List my open Erghi conversations waiting for a human. For each, summarise what the visitor wants
and draft a reply. Don't send anything until I approve each one.
```

## Read-only audit

Run the server with `ERGHI_MCP_READ_ONLY=true` and a key with `view_audit_logs` (plus any read scopes).

```text
Summarise what changed in my Erghi workspace this week from the audit log, and flag anything
done by an API key rather than a person.
```

## What a key can't do

Billing and plan changes, inviting team members and changing roles, OpenAPI integrations, logo upload, custom domains and account deletion stay in the admin app at https://app.erghi.ai. The model will point you there.
