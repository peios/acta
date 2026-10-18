---
title: "Complete Frame Catalog"
description: >-
  Complete frame catalog in Acta, including state, mapping, and failure
  behavior.
---

Each row names the definition in the schema's `$defs`. Each frame definition
combines the common envelope, its literal `kind` and its full data definition.

| Kind | Frame definition | Data definition |
| --- | --- | --- |
| `thread/configuration` | `ConfigurationFrame` | `Configuration` |
| `thread/status` | `ThreadStatusFrame` | `ThreadStatus` |
| `turn/started` | `TurnStartedFrame` | `TurnStarted` |
| `turn/completed` | `TurnCompletedFrame` | `TurnCompleted` |
| `message/user` | `UserMessageFrame` | `UserMessage` |
| `message/assistant` | `AssistantMessageFrame` | `AssistantMessage` |
| `message/assistant/delta` | `AssistantMessageDeltaFrame` | `AssistantMessageDelta` |
| `usage/context` | `ContextUsageFrame` | `ContextUsage` |
| `usage/account` | `AccountUsageFrame` | `AccountUsage` |
| `mcp/server/status` | `MCPServerStatusFrame` | `MCPServerStatus` |
| `approval/request` | `ApprovalRequestFrame` | `ApprovalRequest` |
| `approval/resolved` | `ApprovalResolvedFrame` | `ApprovalResolved` |
| `approval/review` | `ApprovalReviewFrame` | `ApprovalReview` |
| `debug/unknown` | `DebugUnknownFrame` | `DebugUnknown` |
| `debug/dropped` | `DebugDroppedFrame` | `DebugDropped` |
| `debug/local` | `DebugLocalFrame` | `DebugLocal` |
| `debug/resolved` | `DebugResolvedFrame` | `DebugResolved` |
| `debug/provider-diagnostic` | `ProviderDiagnosticFrame` | `ProviderDiagnostic` |

Discovery, connection/process lifecycle, provider initialization metadata, and
commitment remain hypercontrol. Their wire schemas are intentionally outside
this thread-frame document.
