# Working on Acta

Acta is a self-hosted project tool for humans and agents. Give human experience
and agent experience equal importance.

## Collaboration

- Stay in lockstep with Jack. Discuss features and consequential design choices
  together before implementing them. Agreement on direction is not permission
  to invent additional features.
- Design and build component by component, allowing time to review each part.
- Iterate on the visible app at port 8081 so Jack can watch changes as they
  happen. Do not move visual review to a separate preview port.
- Speak up about problems, better approaches, and trade-offs, including problems
  with earlier assistant recommendations. Jack expects independent judgment.

## Quality and maintainability

- Invest in correctness, clarity, maintainability, accessibility and deliberate
  UX. Quality sets the pace; do not rush to cover a broad feature surface.
- Keep shared rules and invariants DRY, with clear ownership and boundaries.
  Prefer understandable code and cohesive components as the system grows.
- When code becomes clumsy or a dedicated refactor pass would improve it, raise
  that with Jack before the pass. He explicitly welcomes and expects such work.
- During this pre-production phase, backward compatibility of Acta code and
  protocols is not a requirement. Replace or remove obsolete implementations
  and contracts when appropriate; do not retain compatibility layers solely to
  preserve earlier iterations.
- PostgreSQL is the chosen database. Isolate database-specific persistence from
  domain and transport logic, and define the transactional guarantees a future
  storage implementation must preserve.

## Coding style

- Follow Panorama PSC common and language coding rules. See
  `docs/learn/site/acta.product/7--development.topic/200--checking.md` for commands
  and exclusions. Keep formatter success separate from design review.
- Run `./bazel run //:format-check`, `./bazel run //:style-check`, and the
  relevant Bazel tests. Frontend tests include formatting and ESLint checks.
- Preserve Svelte reactivity and import initialization order during cleanup.
  Do not reformat checksum-protected SQL migration history.

## Project records

- Keep maintained product documentation in `docs/learn/site/acta.product`, following
  Panorama's [Core Style](../panorama/docs/psc.product/1--technical-writing-and-documentation.shelf/2--style.topic/100--core-style.md)
  and [Learn](../panorama/docs/psc.product/1--technical-writing-and-documentation.shelf/2--style.topic/300--learn.md) rules. Run `trail lint docs/learn/site` and
  `trail build docs/learn/site --strict` with a Trail build supporting attachments.
- When changing agent-facing features, update
  `docs/learn/site/acta.product/3--connections.topic/400--agent-guide.md` and affected
  tool descriptions. The application embeds that page body without Trail
  metadata. Keep that canonical source consistent with the tools.
- Record work, agreed decisions, rationale and validation in the corresponding
  Acta task. Distinguish agreed decisions from open proposals.
