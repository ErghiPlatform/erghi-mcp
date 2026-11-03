# Action policies: hard limits on what the assistant may do

Instructions are advice the model may be talked out of. A policy is checked by Erghi before a protected integration call runs, whatever the model says, and fails closed.

A policy (`create_action_policy`) has:
- `policyKey` (create only, stable id), `name`, `description`, `isActive`.
- `protectedOperations`: operation ids from `list_protected_operations`. An id no active integration offers is refused (a typo would protect nothing).
- At least one of:
  - `requiredSteps`: `[{ "key": "...", "blockMessage": "...", "prerequisiteOperationIds": ["verifyOtp"] }]`. One of the listed operations must have succeeded earlier in the conversation (in any visitor turn, for 24 hours). Each step needs at least one prerequisite, step keys must differ, and a prerequisite cannot be one of the protected operations. The block message tells the model what to do instead.
    Optional per step:
    - `successWhen: { "path": "verified", "equals": "true" }`: the step completes only when the prerequisite's JSON response has that value at that dotted path (compared as text; `true`, `42`). Without it, any HTTP 2xx completes the step, even `200 {"verified": false}`. Set it whenever the verification API reports failure in the body.
    - `target: { "argument": "order_id", "from": "arguments.order_id" }`: binds the completion to one subject. The protected call's `argument` must equal the value taken from the prerequisite call (`arguments.<path>` it was called with, or `response.<path>` it returned). Verifying order 1 then no longer unlocks order 2. A prerequisite call without that value completes nothing.
    - `validForMinutes`: 1 to 1440, default 1440 (24 h). Use 10 to 15 for OTPs. Each step keeps its own expiry; completing one step no longer extends the others.
    - `singleUse: true`: the completion unlocks one protected call, then the step is needed again. It is used up when the gate allows the call, even if the call then fails.
  - `requireVerifiedIdentity: true`: refused unless the conversation has a verified identity (signed identity token).
  - `argumentRules`: `[{ "argument": "user_id", "equals": "{{identity.sub}}" }]`. `argument` can be a dotted body path (`transfer.user_id`). `equals` can use `identity.*`, `metadata.*`, `conversation.id`, never `secret.*`. A missing argument or unresolvable template refuses the call.

A refused call reports the pending step to the model, which explains it to the visitor. Every call a policy covers, allowed or refused, is written to the audit log (`security.policy_tool_allowed` / `security.policy_tool_blocked`, see `list_audit_logs`), and policy updates record the definition before and after.

`update_action_policy` replaces the whole definition: send every field you want to keep.

Before the first call, the model is told which of its tools are protected and what each needs (a line appended to the tool description), so it can do the steps first instead of learning from a refusal. A refusal reaches it as Erghi's own text, saying the call was not made.

## Checking a policy
- `test_action_policy` dry-runs the gate for one call: pass `operationId`, the `arguments`, and the state to assume (`identityVerified`, `identity`, `metadata`, `completedSteps: [{ "key": "...", "target": "1042" }]`). It returns `allowed`, the `pendingStepKey` and the `message` the model would get, and the `policyKeys` that covered the call. It uses the active policies and changes nothing. Test both sides: the call with every step done (allowed), and without each one (refused at that step).
- `get_action_policy_stats` (`days`, default 7) counts allowed and refused calls per policy from the audit log, with refusals by step. A step that refuses nearly every call usually has a `successWhen` path that never matches or a `target` the prerequisite never carries.

## Recommended baseline
| Endpoint kind | Policy |
|---|---|
| Reads personal data | `requireVerifiedIdentity` + `argumentRules` on any unbound user-id argument |
| Changes data (update profile, book, cancel) | Above + an OTP step with `successWhen`, `target` on the record id, `validForMinutes` ~15 |
| Moves money or is irreversible | Above + `singleUse`, and a dedicated verification operation as the prerequisite; consider not exposing it at all |
| Public reads (catalogue, branches, status) | None |

`review_chat_setup` flags data-changing endpoints with no policy, policies that point at operations that no longer exist, steps that can never complete, and steps with no `successWhen`.
