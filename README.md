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
| `manage_integrations` | Connect the product's APIs from OpenAPI documents, set request bindings and AI instructions, test, turn on or off. Never their credentials. |
| `view_audit_logs` | Security and configuration audit log |

The server only offers tools the key's scopes allow. The API checks every scope again on its own side.

## 2. Add it to your MCP client

### Claude Code

Replace `YOUR_CLIENT_ID` and `YOUR_CLIENT_SECRET` with the full values from step 1:

```bash
claude mcp add erghi \
  -e ERGHI_CLIENT_ID=YOUR_CLIENT_ID -e ERGHI_CLIENT_SECRET=YOUR_CLIENT_SECRET \
  -- docker run -i --rm -e ERGHI_CLIENT_ID -e ERGHI_CLIENT_SECRET \
  ghcr.io/erghiplatform/erghi-mcp:latest
```

The `-e` options before `--` store the key in Claude Code's config (`~/.claude.json`); the bare `-e NAME` options after it hand those values to the container. Then run `claude mcp list` and check that `erghi` shows **Connected**.

If you only ever start Claude Code from a terminal, you can leave out the first pair of `-e` options and `export ERGHI_CLIENT_ID=… ERGHI_CLIENT_SECRET=…` in that shell instead, so the secret isn't written to the config. Claude Code started from the desktop app doesn't see your shell's variables, so use the form above there.

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
        "ERGHI_CLIENT_ID": "YOUR_CLIENT_ID",
        "ERGHI_CLIENT_SECRET": "YOUR_CLIENT_SECRET"
      }
    }
  }
}
```

### Podman instead of Docker

Use Podman's full path as the command, for example `/opt/podman/bin/podman` (macOS installer) or `/usr/bin/podman` (Linux); `which podman` prints it. The arguments stay the same. A shell alias such as `alias docker=podman` doesn't work: MCP clients start the server without your shell, so they never see the alias.

### If it doesn't connect

- **`Executable not found in $PATH: "docker"`**: the client can't find Docker. Use the full path to `docker` (`which docker`) or Podman (above) as the command.
- **`401 (invalid_client)` in the server log, or "Connection closed"**: the client ID or secret is wrong, incomplete or still the placeholder, or the key was revoked or has expired. The secret is shown only once; if you don't have it, revoke the key and create a new one.
- **Variables are empty**: the client started the server without the key. Put the key in the server's config (`-e` before `--` for Claude Code, `env` in the JSON) rather than in your shell.

To see the server's own error, run the same command in a terminal with the two variables set; it prints why it stopped.

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

For a full setup, create a key with `configure_ai`, `manage_workspace_identity`, `manage_widgets` and `chat:read`. Add `manage_integrations` if the assistant should call your product's API.

Prompts:

| Prompt | What it does |
|---|---|
| `setup_erghi` | Works through the dashboard checklist end to end (arguments: `company`, `website`, `language`) |
| `configure_ai_assistant` | Tune the persona, instructions, hand-off and model |
| `build_knowledge_base` | Crawl a site, add documents and FAQs, find gaps |
| `brand_widget` | Match the widget's look and messages to your brand |
| `install_widget` | Create or choose a widget, restrict it to your domains, get the snippet |
| `analyze_product` | Study the product (website, docs, API), map what customers ask to knowledge, integrations, policies and hand-off, then apply the plan (arguments: `website`, `api`, `focus`) |
| `connect_integration` | Connect an API safely: choose endpoints, bind the user's identity, write AI instructions, add policies (arguments: `name`, `spec`, `baseUrl`) |

Clients that don't support MCP prompts can paste the example prompts from [docs/example-prompts.md](docs/example-prompts.md). The shortest one:

> Set up Erghi for Acme (https://acme.com, main language Arabic). Start with get_setup_checklist and work through every step that isn't done, checking with me before each change. Finish by showing me the checklist again.

## Tools

| Area | Tools |
|---|---|
| Key, setup and analysis | `get_api_key_info`, `get_setup_checklist`, `review_chat_setup`, `get_erghi_guide` |
| AI | `get_ai_settings`, `list_available_models`, `update_ai_settings`, `get_ai_cost_usage` |
| Knowledge base | `list_knowledge_documents`, `add_knowledge_document`, `add_faq`, `delete_knowledge_document`, `search_knowledge_base`, `crawl_website`, `get_crawl_job`, `cancel_crawl_job`, `schedule_website_crawl`, `list_crawl_schedules`, `disable_crawl_schedule`, `test_assistant` |
| Branding & persona | `get_branding`, `update_branding` |
| Action policies | `list_action_policies`, `list_protected_operations`, `get_action_policy`, `get_action_policy_stats`, `test_action_policy`, `create_action_policy`, `update_action_policy`, `set_action_policy_active`, `delete_action_policy` |
| Channels | `get_channel_status`, `connect_telegram`, `connect_slack`, `connect_whatsapp`, `disconnect_channel` |
| Widgets | `list_widgets`, `get_widget`, `create_widget`, `update_widget`, `deactivate_widget`, `get_widget_embed_code` |
| Integrations | `list_integrations`, `get_integration`, `add_integration_from_openapi`, `replace_integration_spec`, `refresh_integration_endpoints`, `update_integration_base_url`, `set_integration_tool_context`, `set_endpoint_timeout`, `set_integration_active`, `test_integration_endpoint`, `delete_integration` |
| Audit | `list_audit_logs` |
| Conversations | `list_conversations`, `get_conversation`, `send_reply`, `resolve_conversation`, `get_billing_plans` (forwarded from Erghi's hosted MCP endpoint) |

## Everything that shapes a reply

Each visitor message is answered from the persona and instructions, the knowledge base excerpts that match it, the integration tools the model may call (with their bindings and AI instructions), the action policies that gate those calls, the hand-off settings and the model. `get_erghi_guide` returns Erghi's own guide to each part, so the AI client works from how Erghi actually behaves rather than guesses:

| Topic | Covers |
|---|---|
| `how_replies_work` | Every step from message to reply, and the tool that controls each |
| `product_analysis` | How to study a product and map customer questions to Erghi |
| `knowledge` | Reference vs instruction documents, FAQs, crawling, testing |
| `integrations` | Connecting an API; `identity`, `secret`, `metadata` and `conversation` placeholders |
| `policies` | Required steps, verified identity, argument rules |
| `security` | What keys can and can't do |

`review_chat_setup` reads all of it (as far as the key's scopes allow) and returns findings ranked by severity, for example:

- an endpoint where the model fills `user_id` itself instead of a `{{identity.sub}}` binding;
- a binding that holds a literal token;
- a data-changing endpoint with no action policy;
- an empty knowledge base or a widget any site can embed.

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
  - Human-only permissions (billing, users, account deletion) can't be granted to a key at all.
  - With `manage_integrations` a key can connect and configure APIs, but integration credentials, the widget identity secret and API keys stay human-only (403 `api_key_not_allowed`). Once an integration has credentials, a key can't change its base URL, so it can't redirect them to another server.
  - Endpoints are closed to keys unless they are explicitly opened for a scope.
- **Short-lived tokens, fast revocation.** The key is exchanged for a two-hour token and renewed automatically. Revoking or expiring the key in the admin app stops it within about 30 seconds, including tokens already issued.
- **Destructive actions need `confirm=true`.** This covers deletes, disconnects, deactivations, replacing an integration's spec, and testing any integration endpoint that isn't a GET (it runs against the real system). The model is told to set it only after you explicitly agree.
- **Read-only mode** (`ERGHI_MCP_READ_ONLY=true`) for audits and dashboards.
- **No path steering.** Resource ids are validated as UUIDs and channel names against a fixed list before they reach a URL.
- **Bounded output.** API responses over 1 MB are refused, so a single call can't flood the model's context.
- **Secrets stay out of logs and URLs.**
  - Credentials come from the environment or a secrets file.
  - Logs go to stderr and never include the secret.
  - The API URL must be HTTPS.
- **Prompt-injection hygiene.** The server instructions and prompts tell the model to treat page, document and conversation content as data. Treat that as defence in depth, not a guarantee: the scopes you grant are the real limit.
- **Audited.** Changes made through a key are recorded in the workspace audit log under the key's client ID. This covers branding, AI settings, knowledge base, integration, action policy and channel changes, plus key creation and revocation.

### Deliberately not available through a key

Billing and plan changes, team members and roles, integration credentials, the widget identity secret, API keys, logo upload, custom domains, website/site registration, and account deletion. Use the admin app for those.

## Development

```bash
go test ./...
go build ./cmd/erghi-mcp
docker build -t erghi-mcp .
```

Releases: pushing a `vX.Y.Z` tag publishes `ghcr.io/erghiplatform/erghi-mcp` for amd64 and arm64.

## License

MIT
