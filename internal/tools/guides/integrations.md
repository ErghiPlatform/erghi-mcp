# Integrations: letting the assistant call the client's API

An integration is an OpenAPI document plus a base URL. Each operation becomes a tool the model can call during a chat.

## Connect
1. `add_integration_from_openapi` with the spec text, a name and the https base URL. A spec without `operationId`s gets generated names; prefer adding real ones.
2. Credentials (API key, bearer, basic, OAuth2 client credentials) are added **by a person** in the admin app (Integrations). API keys can't read or set them, and can't change the base URL once credentials exist.
3. `get_integration` shows the endpoints, ids and parameters.
4. `test_integration_endpoint` calls the real API with the saved credentials. Non-GET calls need confirm=true.

## Tool context: who fills each parameter
`set_integration_tool_context` sets, for the whole integration or one endpoint (endpoint entries add to or override integration ones):
- `requestBindings`: `[{ "in": "header|query|path|body", "name": "...", "value": "...", "required": true }]`, max 30. A bound parameter is removed from the tool schema, so the model can't set it.
- `aiInstructions`: up to 4000 characters, appended to the tool description: when to use it, units, formats, what to tell the visitor.

Placeholders in `value`:

| Placeholder | Comes from | Trust |
|---|---|---|
| `{{identity.sub}}`, `{{identity.<claim>}}` | A JWT the host app signs with the workspace's widget secret | Verified. **The only safe way to say who the user is.** |
| `{{secret.<key>}}` | The host app via SDK `setSecureContext` (e.g. the user's access token). Kept encrypted in Redis with a TTL, never shown to the model, sent only to the integration's own base URL, redacted from results | Opaque; the client's API validates it |
| `{{metadata.<key>}}` | `identify`/`setContext` on the conversation | **Not trusted**: the visitor can set it |
| `{{conversation.id}}` | Erghi | Yes |

If a required placeholder has no value, the call is not made and the visitor is asked to sign in again. The SDK can refresh tokens without ending the chat.

## Rules of thumb
- Every parameter that picks whose data is touched (`user_id`, `customer_id`, `account_id`…) → bind to `{{identity.sub}}`. Never to `metadata`.
- User-scoped public APIs → `header Authorization = Bearer {{secret.<token_key>}}`.
- Never put a literal credential in a binding value; bindings are readable by anyone who can read the integration.
- Leave search terms, dates and free text unbound so the model fills them from the conversation.
- Back up with an action policy: `requireVerifiedIdentity` and `argumentRules` (`user_id` equals `{{identity.sub}}`) catch anything a binding missed, including body fields.

## Example
```json
{
  "requestBindings": [
    { "in": "header", "name": "Authorization", "value": "Bearer {{secret.app_access_token}}" },
    { "in": "path", "name": "user_id", "value": "{{identity.sub}}" },
    { "in": "query", "name": "lang", "value": "{{metadata.locale}}", "required": false }
  ],
  "aiInstructions": "Use getOrders for 'where is my order'. Amounts are in cents; divide by 100 and show the currency. Never read out full card numbers."
}
```
