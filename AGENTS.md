# Working on Acta 2

Acta 2 is a deliberate rebuild intended to become a substantial product. Give
human experience and agent experience equal importance.

## Collaboration

- Stay in lockstep with Jack. Discuss features and consequential design choices
  together before implementing them. Agreement on direction is not permission
  to invent additional features.
- Design and build component by component, allowing time to review each part.
- Iterate on the visible app at port 8081 so Jack can watch changes as they
  happen. Do not move visual review to a separate preview port.
- Speak up about problems, better approaches, and trade-offs, including problems
  with earlier assistant recommendations. Jack expects independent judgment.
- Approach the product as a fresh design. Do not inspect or reuse old Acta's
  implementation or product decisions unless agreed with Jack.

## Quality and maintainability

- Invest in correctness, clarity, maintainability, accessibility and deliberate
  UX. Quality sets the pace; do not rush to cover a broad feature surface.
- Keep shared rules and invariants DRY, with clear ownership and boundaries.
  Prefer understandable code and cohesive components as the system grows.
- When code becomes clumsy or a dedicated refactor pass would improve it, raise
  that with Jack before the pass. He explicitly welcomes and expects such work.
- During this pre-production phase, backward compatibility of Acta 2 code and
  protocols is not a requirement. Replace or remove obsolete implementations
  and contracts when appropriate; do not retain compatibility layers solely to
  preserve earlier iterations. This applies to the new Acta 2 project.
- PostgreSQL is the chosen database. Isolate database-specific persistence from
  domain and transport logic, and define the transactional guarantees a future
  storage implementation must preserve.

## Project records

- Keep program behaviour and development instructions in this project's learn/.
- When adding or changing agent-facing features, update learn/mcp-agent-guide.md
  and the affected tool descriptions alongside the implementation. The MCP guide
  is embedded directly from that Markdown file; keep its examples and behavioral
  guidance consistent with the actual tools.
- Record work, agreed decisions, rationale and validation in the corresponding
  Acta task. Distinguish agreed decisions from open proposals.
- The initial empty-page foundation is ACT-50 (528cdm65). Account,
  authentication and permission design is ACT-51 (b29dwnb2) in the acta workspace.
