package postgres

import (
	"acta2/internal/auth"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"regexp"
	"strings"
)

var proseReference = regexp.MustCompile(`(^|[^A-Za-z0-9_./@-])(@[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*)?|[A-Z]{2,10}-[1-9][0-9]*)(\b)`)

// Only prose text is resolved: code, existing links and HTML remain untouched.
// Stable URLs carry identity; their labels preserve the author's wording.
func (t workspaceTx) TaskDescription(ctx context.Context, w, markdown string) (string, error) {
	source := []byte(markdown)
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	type edit struct {
		start, end int
		value      string
	}
	edits := []edit{}
	var resolveErr error
	ast.Walk(doc, func(n ast.Node, enter bool) (ast.WalkStatus, error) {
		if !enter {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindCodeSpan, ast.KindLink, ast.KindImage, ast.KindAutoLink, ast.KindHTMLBlock, ast.KindRawHTML:
			return ast.WalkSkipChildren, nil
		}
		v, ok := n.(*ast.Text)
		if !ok {
			return ast.WalkContinue, nil
		}
		segment := source[v.Segment.Start:v.Segment.Stop]
		for _, m := range proseReference.FindAllSubmatchIndex(segment, -1) {
			label := string(segment[m[4]:m[5]])
			href := ""
			if strings.HasPrefix(label, "@") {
				var id string
				e := t.tx.QueryRow(ctx, `SELECT account_id::text FROM account_handles WHERE handle=$1`, strings.ToLower(label[1:])).Scan(&id)
				if errors.Is(e, pgx.ErrNoRows) {
					continue
				}
				if e != nil {
					resolveErr = e
					return ast.WalkStop, e
				}
				p, e := t.person(ctx, id, w)
				if e != nil {
					resolveErr = e
					return ast.WalkStop, e
				}
				if !p.Available {
					continue
				}
				href = "/references/accounts/" + id
			} else {
				task, e := t.TaskGet(ctx, label)
				if errors.Is(e, auth.ErrNotFound) {
					continue
				}
				if e != nil {
					resolveErr = e
					return ast.WalkStop, e
				}
				access, e := t.Access(ctx, t.actor, task.WorkspaceID)
				if e != nil {
					resolveErr = e
					return ast.WalkStop, e
				}
				if !access.Allowed {
					continue
				}
				href = "/tasks/" + task.ID
			}
			edits = append(edits, edit{v.Segment.Start + m[4], v.Segment.Start + m[5], "[" + label + "](" + href + ")"})
		}
		return ast.WalkContinue, nil
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		markdown = markdown[:e.start] + e.value + markdown[e.end:]
	}
	return markdown, nil
}
