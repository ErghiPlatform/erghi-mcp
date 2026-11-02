# What an API key can and can't do

Scopes on the key decide which tools exist:

| Scope | Unlocks |
|---|---|
| `chat:read`, `chat:write` | Read conversations, reply, resolve (hosted tools) |
| `configure_ai` | Assistant instructions and AI settings, knowledge base, action policies, channels |
| `manage_workspace_identity` | Company name, colours, links |
| `manage_widgets` | Widgets and embed code |
| `manage_integrations` | Integrations: specs, tool context, activate, test, delete |
| `view_audit_logs` | Audit log |

Always people-only, through the admin app: billing and plans, team members and roles, API keys, the widget identity secret, integration credentials, and changing the base URL of an integration that has credentials. Changes to integrations, action policies and keys are recorded in the audit log, with the key as the actor.

The assistant's instructions are not secret from visitors. Secrets belong in integration credentials or `{{secret.*}}` values from the host app.
