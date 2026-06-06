package board

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/peios/acta/internal/store"
)

// ErrInvalidAttribute is returned when a priority/type/size value isn't one of
// the vocabulary's known values.
var ErrInvalidAttribute = errors.New("board: invalid attribute value")

// AttrOption is one value in a fixed item-attribute enum (priority, type, size).
// Value is the int persisted on the item (0 = unset); Slug is the stable token
// used in URLs, view filters and the MCP surface; Label is the human display.
// Presentation (colour, glyph) is driven off Slug in the templates/CSS, so this
// stays pure data.
type AttrOption struct {
	Value int
	Slug  string
	Label string
}

// AttrVocab is a small fixed enum with O(1) lookup by value and by slug. The
// option order is display order (what menus render top-to-bottom); the "unset"
// option is included and listed last as the clear choice.
type AttrVocab struct {
	options []AttrOption
	byValue map[int]AttrOption
	bySlug  map[string]AttrOption
}

func newAttrVocab(opts []AttrOption) AttrVocab {
	v := AttrVocab{options: opts, byValue: make(map[int]AttrOption, len(opts)), bySlug: make(map[string]AttrOption, len(opts))}
	for _, o := range opts {
		v.byValue[o.Value] = o
		v.bySlug[o.Slug] = o
	}
	return v
}

// Options returns the vocabulary in display order (template menus range over it).
func (v AttrVocab) Options() []AttrOption { return v.options }

// Valid reports whether value is a known option (0 is valid — it's "unset").
func (v AttrVocab) Valid(value int) bool { _, ok := v.byValue[value]; return ok }

// Option returns the option for a value, or the unset option for an unknown one.
func (v AttrVocab) Option(value int) AttrOption {
	if o, ok := v.byValue[value]; ok {
		return o
	}
	return v.byValue[0]
}

// Label is the display label for a value ("No priority" etc. for 0).
func (v AttrVocab) Label(value int) string { return v.Option(value).Label }

// Slug is the stable token for a value ("" for an unknown value; "none" for 0).
func (v AttrVocab) Slug(value int) string { return v.Option(value).Slug }

// Parse maps a slug back to its value. Whitespace and case are ignored. The bool
// is false for an unknown slug (so callers can reject rather than silently unset).
func (v AttrVocab) Parse(slug string) (int, bool) {
	o, ok := v.bySlug[strings.ToLower(strings.TrimSpace(slug))]
	return o.Value, ok
}

// The canonical attribute vocabularies. Real options first, "unset" last (the
// clear choice in a picker). These are the single source of truth shared by the
// web handlers, the view-filter facets and the MCP surface.
var (
	Priorities = newAttrVocab([]AttrOption{
		{4, "urgent", "Urgent"},
		{3, "high", "High"},
		{2, "medium", "Medium"},
		{1, "low", "Low"},
		{0, "none", "No priority"},
	})
	ItemTypes = newAttrVocab([]AttrOption{
		{1, "feature", "Feature"},
		{2, "bug", "Bug"},
		{3, "chore", "Chore"},
		{0, "none", "No type"},
	})
	Sizes = newAttrVocab([]AttrOption{
		{1, "xs", "XS"},
		{2, "s", "S"},
		{3, "m", "M"},
		{4, "l", "L"},
		{5, "xl", "XL"},
		{0, "none", "No size"},
	})
)

// --- due dates ---

// dateLayout is the wire/URL form of a due date (date-only).
const dateLayout = "2006-01-02"

// ParseDue parses a "YYYY-MM-DD" string into a UTC-midnight time. An empty string
// yields (nil, nil) — no due date. A malformed string is an error.
func ParseDue(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

// DueString formats a due date as "YYYY-MM-DD", or "" for nil.
func DueString(due *time.Time) string {
	if due == nil {
		return ""
	}
	return due.UTC().Format(dateLayout)
}

// normalizeDue truncates a due date to UTC midnight so comparisons are by day.
func normalizeDue(due *time.Time) *time.Time {
	if due == nil {
		return nil
	}
	u := due.UTC()
	d := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}

func sameDay(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.UTC().Year() == b.UTC().Year() && a.UTC().YearDay() == b.UTC().YearDay()
}

// Overdue reports whether a due date is in the past and the item isn't done.
// A done item is never overdue (its deadline no longer matters), and an item with
// no due date never is. "Past" is strictly before today (UTC) — due today is not
// yet overdue.
func Overdue(due *time.Time, done bool) bool {
	if due == nil || done {
		return false
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return normalizeDue(due).Before(today)
}

// --- service ---

// SetPriority sets an item's priority (0 clears it); out-of-range is rejected.
func (s *Service) SetPriority(ctx context.Context, id string, priority int) error {
	return s.setEnumAttr(ctx, id, priority, Priorities, store.EventItemPriority,
		func(it store.Item) int { return it.Priority }, s.store.SetItemPriority)
}

// SetType sets an item's type (0 clears it); out-of-range is rejected.
func (s *Service) SetType(ctx context.Context, id string, itemType int) error {
	return s.setEnumAttr(ctx, id, itemType, ItemTypes, store.EventItemType,
		func(it store.Item) int { return it.Type }, s.store.SetItemType)
}

// SetSize sets an item's size (0 clears it); out-of-range is rejected.
func (s *Service) SetSize(ctx context.Context, id string, size int) error {
	return s.setEnumAttr(ctx, id, size, Sizes, store.EventItemSize,
		func(it store.Item) int { return it.Size }, s.store.SetItemSize)
}

// setEnumAttr is the shared body of the three enum setters: validate the value,
// no-op (and emit nothing) when unchanged, persist, then log an event whose "to"
// is the new label ("" when cleared).
func (s *Service) setEnumAttr(ctx context.Context, id string, value int, vocab AttrVocab, verb string,
	current func(store.Item) int, set func(context.Context, string, int) error) error {
	if !vocab.Valid(value) {
		return ErrInvalidAttribute
	}
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	if current(item) == value {
		return nil
	}
	if err := set(ctx, id, value); err != nil {
		return err
	}
	label := ""
	if value != 0 {
		label = vocab.Label(value)
	}
	s.recordEvent(ctx, item, verb, map[string]string{"to": label})
	return nil
}

// SetDue sets (or clears, with nil) an item's target date. The date is normalised
// to a UTC day; an unchanged date is a no-op.
func (s *Service) SetDue(ctx context.Context, id string, due *time.Time) error {
	item, err := s.store.ItemByID(ctx, id)
	if err != nil {
		return err
	}
	due = normalizeDue(due)
	if sameDay(item.DueDate, due) {
		return nil
	}
	if err := s.store.SetItemDue(ctx, id, due); err != nil {
		return err
	}
	s.recordEvent(ctx, item, store.EventItemDue, map[string]string{"to": DueString(due)})
	return nil
}
