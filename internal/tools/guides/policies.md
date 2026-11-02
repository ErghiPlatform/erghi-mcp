# Action policies: hard limits on what the assistant may do

Instructions are advice the model may be talked out of. A policy is checked by Erghi before a protected integration call runs, whatever the model says, and fails closed.

A policy (`create_action_policy`) has:
- `policyKey` (create only, stable id), `name`, `description`, `isActive`.
- `protectedOperations`: operation ids from `list_protected_operations`.
- At least one of:
  - `requiredSteps`: `[{ "key": "...", "blockMessage": "...", "prerequisiteOperationIds": ["verifyOtp"] }]`. One of the listed operations must have succeeded earlier in the conversation. The block message tells the model what to do instead.
  - `requireVerifiedIdentity: true`: refused unless the conversation has a verified identity (signed identity token).
  - `argumentRules`: `[{ "argument": "user_id", "equals": "{{identity.sub}}" }]`. `argument` can be a dotted body path (`transfer.user_id`). `equals` can use `identity.*`, `metadata.*`, `conversation.id`, never `secret.*`. A missing argument or unresolvable template refuses the call.

A refused call reports the pending step to the model, which explains it to the visitor.

## Recommended baseline
| Endpoint kind | Policy |
|---|---|
| Reads personal data | `requireVerifiedIdentity` + `argumentRules` on any unbound user-id argument |
| Changes data (update profile, book, cancel) | Above + a confirmation or OTP step |
| Moves money or is irreversible | Above + a dedicated verification operation as a prerequisite; consider not exposing it at all |
| Public reads (catalogue, branches, status) | None |

`review_chat_setup` flags data-changing endpoints with no policy and policies that point at operations that no longer exist.
