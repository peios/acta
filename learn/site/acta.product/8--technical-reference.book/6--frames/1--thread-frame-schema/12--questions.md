---
title: "Questions"
description: >-
  Provider question identities, supported answer forms, immutable replies,
  and resolution.
---

`question/request` contains question_id (the run-bound request UUID), nullable
turn_id/tool_id, blocking, and questions. Each question has id, header, text,
multiple, and options (label/description). Custom text is accepted as an
answer. `question/resolved` contains question_id; it closes the original
persisted request. Pending requests are in current state independently of
paginated history.

The `answer` hypercontrol command uses id = question_id, current run_id, and
an answers object mapping question IDs to arrays of strings. Its durable
result retains those answers. This shares the approval command's immutable
decision and pipe-write retry machinery, but is not an approve/deny decision.
The local harness rejects replies to expired or replaced requests and never
trusts browser-supplied provider input. Single-choice questions accept one
string; multiple-choice answers can contain several selections and custom
text.

Claude's can_use_tool/AskUserQuestion envelope maps question positions to
stable q1, q2 identifiers within the request. The local reply retains the
original questions and maps their text to answer strings (multiple values
joined by comma and space), per the [Agent SDK user-input
contract](https://code.claude.com/docs/en/agent-sdk/user-input). Codex
item/tool/requestUserInput retains the provider question IDs and replies with
answers[id].answers, as defined by the installed app-server JSON schema.
Codex's isBlocking flag controls whether turn completion closes an unanswered
request. Provider cancellation/resolution always closes it.
