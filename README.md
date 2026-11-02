# Erghi MCP server

Set up and run an [Erghi](https://erghi.ai) workspace from any MCP client (Claude Desktop, Claude Code, Cursor, VS Code…) without opening the admin app. It covers the AI assistant, knowledge base, branding, chat widgets, messaging channels, action policies, audit log and conversations.

It runs as a small Docker container (about 12 MB, distroless, non-root) and authenticates with an **Erghi workspace API key**.

## 1. Create an API key

In the Erghi admin app, go to **Settings → Agent API keys → Create key**. Then:

- Pick only the scopes you need. You can't give a key a permission you don't have yourself.
- Copy the client ID and secret. The secret is shown once.

| Scope | Unlocks |
|---|---|
| `chat:read` | Read conversations and messages |
| `chat:write` | Reply to and resolve conversations |
| `configure_ai` | Assistant model and persona, knowledge base, website crawls, channels (Telegram/Slack/WhatsApp), action policies |
| `manage_workspace_identity` | Company name, colours, widget theme, legal and support links |
| `manage_widgets` | Create, configure and deactivate chat widgets; embed code |
| `view_audit_logs` | Security and configuration audit log |

The server only offers tools the key's scopes allow. The API checks every scope again on its own side.

## 2. Add it to your MCP client

### Claude Code

```bash
claude mcp add erghi -- docker run -i --rm \
  -e ERGHI_CLIENT_ID -e ERGHI_CLIENT_SECRET \
  ghcr.io/erghiplatform/erghi-mcp:latest
```

Export `ERGHI_CLIENT_ID` and `ERGHI_CLIENT_SECRET` in the shell that starts Claude Code. `-e NAME` with no value passes the variable through without writing the secret into the config file.

### Claude Desktop / Cursor / any stdio client

```json
{
  "mcpServers": {
    "erghi": {
      "command": "docker",
      "args": ["run", "-i", "--rm",
               "-e", "ERGHI_CLIENT_ID", "-e", "ERGHI_CLIENT_SECRET",
               "ghcr.io/erghiplatform/erghi-mcp:latest"],
      "env": {
        "ERGHI_CLIENT_ID": "cf_client_live_…",
        "ERGHI_CLIENT_SECRET": "…"
      }
    }
  }
}
```

### Without Docker

```bash
go install github.com/ErghiPlatform/erghi-mcp/cmd/erghi-mcp@latest
```

Then use `erghi-mcp` as the command.

## 3. Set up your workspace with a prompt

Right after sign-up, the Erghi dashboard shows a **Get your assistant live** checklist. The `setup_erghi` prompt works through the same checklist from your MCP client, and `get_setup_checklist` reports where you are:

| Dashboard step | How the MCP server does it | Scope needed |
|---|---|---|
| *(before the checklist)* Name the assistant and give it instructions | `update_branding` | `configure_ai` |
| Add knowledge | `crawl_website`, `add_faq`, `add_knowledge_document`, then `test_assistant` | `configure_ai` |
| Create a widget | `create_widget`, `get_widget_embed_code` | `manage_widgets` |
| Invite a teammate | Not possible with a key (team members are human-only). The checklist links to the admin app. | n/a |
| Get your first conversation | You send a message through the widget on your site; `get_setup_checklist` and `get_conversation` confirm it | `chat:read` |

For a full setup, create a key with `configure_ai`, `manage_workspace_identity`, `manage_widgets` and `chat:read`.

Prompts:

| Prompt | What it does |
|---|---|
| `setup_erghi` | Works through the dashboard checklist end to end (arguments: `company`, `website`, `language`) |
| `configure_ai_assistant` | Tune the persona, instructions, hand-off and model |
| `build_knowledge_base` | Crawl a site, add documents and FAQs, find gaps |
| `brand_widget` | Match the widget's look and messages to your brand |
| `install_widget` | Create or choose a widget, restrict it to your domains, get the snippet |

Clients that don't support MCP prompts can paste the example prompts from [docs/example-prompts.md](docs/example-prompts.md). The shortest one:

> Set up Erghi for Acme (https://acme.com, main language Arabic). Start with get_setup_checklist and work through every step that isn't done, checking with me before each change. Finish by showing me the checklist again.

## Tools

| Area | Tools |
|---|---|
| Key and setup | `get_api_key_info`, `get_setup_checklist` |
| AI | `get_ai_settings`, `list_available_models`, `update_ai_settings`, `get_ai_cost_usage` |
| Knowledge base | `list_knowledge_documents`, `add_knowledge_document`, `add_faq`, `delete_knowledge_document`, `search_knowledge_base`, `crawl_website`, `get_crawl_job`, `cancel_crawl_job`, `schedule_website_crawl`, `list_crawl_schedules`, `disable_crawl_schedule`, `test_assistant` |
| Branding & persona | `get_branding`, `update_branding` |
| Action policies | `list_action_policies`, `list_protected_operations`, `get_action_policy`, `create_action_policy`, `update_action_policy`, `set_action_policy_active`, `delete_action_policy` |
| Channels | `get_channel_status`, `connect_telegram`, `connect_slack`, `connect_whatsapp`, `disconnect_channel` |
| Widgets | `list_widgets`, `get_widget`, `create_widget`, `update_widget`, `deactivate_widget`, `get_widget_embed_code` |
| Audit | `list_audit_logs` |
| Conversations | `list_conversations`, `get_conversation`, `send_reply`, `resolve_conversation`, `get_billing_plans` (forwarded from Erghi's hosted MCP endpoint) |

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `ERGHI_CLIENT_ID` | (none, required) | API key client ID |
| `ERGHI_CLIENT_SECRET` / `ERGHI_CLIENT_SECRET_FILE` | (none, required) | Secret, or a path to a file holding it (Docker/Kubernetes secrets) |
| `ERGHI_API_URL` | `https://api.erghi.ai` | Must be `https` (plain `http` is accepted for `localhost` only) |
| `ERGHI_WIDGET_HOST` | `https://chat.erghi.ai` | Used in embed snippets |
| `ERGHI_APP_URL` | `https://app.erghi.ai` | Admin app, linked for steps a key can't do |
| `ERGHI_MCP_READ_ONLY` | `false` | `true` offers only tools that change nothing |
| `ERGHI_MCP_TRANSPORT` | `stdio` | `http` serves streamable HTTP at `/mcp` |
| `ERGHI_MCP_HTTP_ADDR` | `127.0.0.1:8080` | Listen address in HTTP mode. Use `0.0.0.0:8080` inside a container. |
| `ERGHI_MCP_HTTP_TOKEN` / `_FILE` | (none) | Required in HTTP mode, at least 32 characters. Clients send `Authorization: Bearer <token>`. |

HTTP mode, for a shared or remote setup:

```bash
docker run --rm -p 127.0.0.1:8080:8080 \
  -e ERGHI_CLIENT_ID -e ERGHI_CLIENT_SECRET \
  -e ERGHI_MCP_TRANSPORT=http -e ERGHI_MCP_HTTP_ADDR=0.0.0.0:8080 \
  -e ERGHI_MCP_HTTP_TOKEN="$(openssl rand -hex 32)" \
  ghcr.io/erghiplatform/erghi-mcp:latest
```

Put it behind TLS before you expose it beyond localhost. Anyone holding the HTTP token can act with the API key's permissions.

## Security model

- **Least privilege.** A key holds only the scopes it was created with, only in its own workspace.
  - Human-only permissions (billing, users, integrations, account deletion) can't be granted to a key at all.
  - Endpoints are closed to keys unless they are explicitly opened for a scope.
- **Short-lived tokens, fast revocation.** The key is exchanged for a two-hour token and renewed automatically. Revoking or expiring the key in the admin app stops it within about 30 seconds, including tokens already issued.
- **Destructive actions need `confirm=true`.** This covers deletes, disconnects and deactivations. The model is told to set it only after you explicitly agree.
- **Read-only mode** (`ERGHI_MCP_READ_ONLY=true`) for audits and dashboards.
- **No path steering.** Resource ids are validated as UUIDs and channel names against a fixed list before they reach a URL.
- **Bounded output.** API responses over 1 MB are refused, so a single call can't flood the model's context.
- **Secrets stay out of logs and URLs.**
  - Credentials come from the environment or a secrets file.
  - Logs go to stderr and never include the secret.
  - The API URL must be HTTPS.
- **Prompt-injection hygiene.** The server instructions and prompts tell the model to treat page, document and conversation content as data. Treat that as defence in depth, not a guarantee: the scopes you grant are the real limit.
- **Audited.** Changes made through a key are recorded in the workspace audit log under the key's client ID. This covers branding, AI settings, knowledge base and channel changes, plus key creation and revocation.

### Deliberately not available through a key

Billing and plan changes, team members and roles, OpenAPI integrations and their credentials, logo upload, custom domains, website/site registration, and account deletion. Use the admin app for those.

## Development

```bash
go test ./...
go build ./cmd/erghi-mcp
docker build -t erghi-mcp .
```

Releases: pushing a `vX.Y.Z` tag publishes `ghcr.io/erghiplatform/erghi-mcp` for amd64 and arm64.

## License

MIT
