# Pre-migration conversation oracle

These frozen functions describe the browser assembler before conversation state
moved to PostgreSQL. They are not imported by application code or shipped in the
browser. They preserve the earlier behavioural tests and allow offline comparison
against the Go reducer with identical captured inputs. Add production behaviour
to `internal/conversation`, not this reference implementation.
