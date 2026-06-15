// Package mcpcfg owns the configurable surface of the MCP integration: the set
// of user-defined prompts (surfaced to clients as slash commands).
//
// The conventions guide served as the acta://guide resource is NOT configurable
// — it ships hardcoded (guide.md, rendered via RenderGuide) and updates with each
// release, like a system prompt that is the same for every instance. Prompts, by
// contrast, are seeded once (DefaultPrompts) as ordinary, editable rows — see
// EnsureSeeded.
package mcpcfg

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/peios/acta/internal/store"
)

// Setting key in the store's app_settings bag.
const seededKey = "mcp.seeded"

// Field limits. Generous — these guard against runaway input, not real use.
const (
	MaxName        = 64
	MaxTitle       = 80
	MaxDescription = 300
	MaxBody        = 100_000
	MaxArgs        = 16
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidationError is a user-correctable problem with submitted prompt input.
// Handlers surface its message; anything else is an internal error.
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error {
	return ValidationError{Msg: fmt.Sprintf(format, a...)}
}

// Service is the read/write API for the guide and prompts.
type Service struct {
	store store.Store
}

func New(st store.Store) *Service { return &Service{store: st} }

// The conventions guide is not stored or configurable — it is served straight
// from the hardcoded guide.md, rendered with live context by RenderGuide (see
// defaults.go and mcp_resources.go).

// --- prompts ---

// PromptInput is the raw form a prompt is created/updated from. ArgsText is the
// textarea format: one argument per line, "name: description", with a trailing
// "*" on the name marking it required.
type PromptInput struct {
	Name        string
	Title       string
	Description string
	Body        string
	ArgsText    string
}

// Prompts lists all prompts in display order.
func (s *Service) Prompts(ctx context.Context) ([]store.MCPPrompt, error) {
	return s.store.ListMCPPrompts(ctx)
}

// Prompt fetches one by id.
func (s *Service) Prompt(ctx context.Context, id string) (store.MCPPrompt, error) {
	return s.store.MCPPromptByID(ctx, id)
}

// CreatePrompt validates and stores a new prompt, appended after existing ones.
func (s *Service) CreatePrompt(ctx context.Context, in PromptInput) (store.MCPPrompt, error) {
	p, err := s.build(in)
	if err != nil {
		return store.MCPPrompt{}, err
	}
	existing, err := s.store.ListMCPPrompts(ctx)
	if err != nil {
		return store.MCPPrompt{}, err
	}
	p.Position = len(existing)
	out, err := s.store.CreateMCPPrompt(ctx, p)
	if errors.Is(err, store.ErrMCPPromptNameTaken) {
		return store.MCPPrompt{}, invalid("a prompt named %q already exists", p.Name)
	}
	return out, err
}

// UpdatePrompt validates and overwrites the prompt's mutable fields, keeping its
// position.
func (s *Service) UpdatePrompt(ctx context.Context, id string, in PromptInput) error {
	cur, err := s.store.MCPPromptByID(ctx, id)
	if err != nil {
		return err
	}
	p, err := s.build(in)
	if err != nil {
		return err
	}
	p.ID = id
	p.Position = cur.Position
	err = s.store.UpdateMCPPrompt(ctx, p)
	if errors.Is(err, store.ErrMCPPromptNameTaken) {
		return invalid("a prompt named %q already exists", p.Name)
	}
	return err
}

// DeletePrompt removes a prompt.
func (s *Service) DeletePrompt(ctx context.Context, id string) error {
	return s.store.DeleteMCPPrompt(ctx, id)
}

// build validates a PromptInput and turns it into a store.MCPPrompt (no id/pos).
func (s *Service) build(in PromptInput) (store.MCPPrompt, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return store.MCPPrompt{}, invalid("a prompt name is required")
	}
	if len(name) > MaxName || !nameRe.MatchString(name) {
		return store.MCPPrompt{}, invalid("name %q must be lowercase letters, digits and underscores, starting with a letter", in.Name)
	}
	title := strings.TrimSpace(in.Title)
	if len(title) > MaxTitle {
		return store.MCPPrompt{}, invalid("title is too long (max %d characters)", MaxTitle)
	}
	desc := strings.TrimSpace(in.Description)
	if len(desc) > MaxDescription {
		return store.MCPPrompt{}, invalid("description is too long (max %d characters)", MaxDescription)
	}
	body := strings.TrimRight(in.Body, "\n ")
	if strings.TrimSpace(body) == "" {
		return store.MCPPrompt{}, invalid("a prompt body is required")
	}
	if len(body) > MaxBody {
		return store.MCPPrompt{}, invalid("body is too long (max %d characters)", MaxBody)
	}
	args, err := ParseArgs(in.ArgsText)
	if err != nil {
		return store.MCPPrompt{}, err
	}
	return store.MCPPrompt{
		Name:        name,
		Title:       title,
		Description: desc,
		Body:        body,
		Arguments:   args,
	}, nil
}

// ParseArgs reads the textarea argument format into structured arguments. Each
// non-blank line is "name: description"; a trailing "*" on the name (before the
// colon) marks the argument required. A line with no colon is a bare name.
func ParseArgs(text string) ([]store.MCPPromptArg, error) {
	var out []store.MCPPromptArg
	seen := map[string]bool{}
	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		namePart, desc, _ := strings.Cut(line, ":")
		namePart = strings.TrimSpace(namePart)
		desc = strings.TrimSpace(desc)
		required := strings.HasSuffix(namePart, "*")
		name := strings.TrimSpace(strings.TrimSuffix(namePart, "*"))
		if !nameRe.MatchString(name) || len(name) > MaxName {
			return nil, invalid("argument name %q must be lowercase letters, digits and underscores, starting with a letter", namePart)
		}
		if seen[name] {
			return nil, invalid("argument %q is listed twice", name)
		}
		if len(desc) > MaxDescription {
			return nil, invalid("description for argument %q is too long (max %d characters)", name, MaxDescription)
		}
		seen[name] = true
		out = append(out, store.MCPPromptArg{Name: name, Description: desc, Required: required})
		if len(out) > MaxArgs {
			return nil, invalid("too many arguments (max %d)", MaxArgs)
		}
	}
	return out, nil
}

// FormatArgs renders structured arguments back into the textarea format, for
// pre-filling the edit form.
func FormatArgs(args []store.MCPPromptArg) string {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(a.Name)
		if a.Required {
			b.WriteString("*")
		}
		if a.Description != "" {
			b.WriteString(": ")
			b.WriteString(a.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// EnsureSeeded inserts the DefaultPrompts once, the first time it runs against a
// fresh database. It is idempotent: a 'mcp.seeded' flag guards re-seeding, and a
// name collision on an individual prompt is ignored, so a half-seeded run heals.
func (s *Service) EnsureSeeded(ctx context.Context) error {
	done, err := s.store.AppSetting(ctx, seededKey)
	if err != nil {
		return err
	}
	if done == "1" {
		return nil
	}
	for i, p := range DefaultPrompts {
		p.Position = i
		_, err := s.store.CreateMCPPrompt(ctx, p)
		if err != nil && !errors.Is(err, store.ErrMCPPromptNameTaken) {
			return err
		}
	}
	return s.store.SetAppSetting(ctx, seededKey, "1")
}
