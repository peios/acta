# Provider permission mapping investigation

2026-09-07. Read-only contract investigation for the generic thread-frame slice.
No provider settings, application code, credentials or running threads changed.
Installed versions checked: Codex 0.153.4 and Claude Code 2.1.263.

## Decision

Jack accepted the candidate mappings below as close enough for Acta's common
categories. Native differences documented here are acceptable; do not require
identical enforcement behaviour or classify the accepted combinations as custom
merely because their providers differ. Rename preapproved_only to dont_ask.
Retain the separate access summary, so a bypass mapping does not conceal the
associated unrestricted Codex configuration. Genuinely different configurations
remain custom; unavailable information remains unknown. No provider configuration
changes are authorized by this reporting decision.

## Finding

The providers support a common approval vocabulary, but their native modes are
not interchangeable security contracts. A mode may describe who reviews actions
requiring approval, without saying which actions reach that review. Acta must
keep those concepts distinct from filesystem/network boundaries.

The initial claim that Codex has no edit-accepting combination was too strong.
Its documented workspace-write + untrusted combination automatically permits
edits and prompts for untrusted commands. That is a close candidate, not a proof
of equivalence to Claude acceptEdits, which also permits certain mutating
filesystem commands. The difference matters when defining the common guarantee.

## Candidate mappings and limits

| Acta draft mode | Claude | Codex | Assessment |
| --- | --- | --- | --- |
| ask | manual/default | on-request + user reviewer | Good common label if it means human review of eligible prompts, not approval of every mutation. Native baseline permissions differ. |
| accept_edits | acceptEdits | workspace-write + untrusted | Close but not exact. Command classifications and exceptions differ. Do not advertise an exact cross-provider mapping yet. |
| automatic | auto, when available | on-request + auto_review | Both delegate eligible review to an automatic reviewer. They do not inspect the same action set or use the same policy. |
| preapproved_only | dontAsk | never with existing sandbox | Both offer no additional approval. Codex can execute anything already permitted by its sandbox/rules; this is not necessarily an explicit tool allowlist. Rename to dont_ask is proposed. |
| bypass | bypassPermissions | never plus unrestricted sandbox is the closest broad combination | Not independent of access configuration on Codex. No faithful standalone bypass switch preserving arbitrary boundaries was established. Native managed/deny protections can still apply. |

`custom` means known configuration outside a faithfully mapped preset; `unknown`
means insufficient evidence to classify it. Neither should be offered as a
permission-granting action. A supported type in generated schema is evidence of
API shape, not proof of successful runtime operation or policy availability.

Claude's installed help advertises acceptEdits, auto, bypassPermissions, manual,
dontAsk and plan. Its newer permission-prompts host/none option changes who can
answer otherwise-required prompts independently of the permission mode. This is
another reason not to infer the full effective policy from a mode string alone.

Codex's installed app-server schema accepts untrusted, on-request, granular and
never; the CLI help lists fewer choices. The integration uses app-server, so its
schema and resolved response are the relevant interface. Automatic approval
review is separate from when a prompt is generated, and per-tool app settings
can override the top-level reviewer. Top-level mode is a default summary, not a
guarantee covering every tool or hook.

## Filesystem/network and planning

- Codex workspace-write includes temporary roots and protected exceptions. A
  workspace_write summary must not promise writes are confined solely to cwd.
- Claude tool permissions and its optional Bash sandbox have different coverage.
  Do not claim whole-provider filesystem/network isolation from the Bash sandbox
  alone. Effective scope must be established; otherwise report custom/unknown.
- Keep plan as work_mode. Claude plan is a workflow/tool policy; Codex read-only
  sandbox alone does not establish an equivalent planning workflow. Its actual
  collaboration-mode mapping is outside this permission investigation.

## Recommendation for this reporting-only slice

Keep the approved shape while settling its meaning. Use ask and automatic as
review-routing categories with provider-specific baselines; propose renaming
preapproved_only to dont_ask. Keep accept_edits/bypass in the vocabulary but do
not force them onto provider combinations that cannot preserve the defined
semantics. Retain original details in debug records. Do not invent extra
hyperharness approval enforcement or change the user's provider settings in
order to make reporting fit a category.

For future controls, supported-mode capabilities must depend on the installed
provider and its restrictions. Test those contracts before exposing switches;
the current investigation does not claim an end-to-end enforcement proof.

## Sources

- [Claude permission modes](https://code.claude.com/docs/en/permission-modes)
- [Claude SDK permission evaluation](https://code.claude.com/docs/en/agent-sdk/permissions)
- [Codex sandbox and approvals](https://learn.chatgpt.com/docs/agent-approvals-security)
- [Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference)
- Local generated AskForApproval, ApprovalsReviewer, ThreadStartParams and
  SandboxPolicy types; local codex/claude help. No live model calls were made.
