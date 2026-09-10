package postgres

import (
	"acta2/internal/tasks"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"regexp"
	"strconv"
	"strings"
)

var searchReference = regexp.MustCompile(`^([A-Za-z]{2,10})-([1-9][0-9]*)$`)

// Result identity is a task, not a matching document. Rank and page only after
// selecting the best source for each task. Excerpts are generated for that page.
func (t workspaceTx) SearchTasks(ctx context.Context, allowed []string, q tasks.SearchQuery, p tasks.SearchPosition) (tasks.SearchPage, error) {
	out := tasks.SearchPage{Tasks: []tasks.SearchResult{}}
	if len(allowed) == 0 {
		return out, nil
	}
	prefix := ""
	var number int64
	if m := searchReference.FindStringSubmatch(q.Query); m != nil {
		prefix = strings.ToUpper(m[1])
		var e error
		number, e = strconv.ParseInt(m[2], 10, 64)
		if e != nil {
			number = 0
		}
	}
	id := ""
	if parsed, e := uuid.Parse(q.Query); e == nil {
		id = parsed.String()
	}
	rows, e := t.tx.Query(ctx, `WITH RECURSIVE query AS (
 SELECT plainto_tsquery('english',$2) AS full,plainto_tsquery('simple',$2) AS title,
 COALESCE((SELECT to_tsquery('simple',string_agg(quote_literal(v)||':*',' & ')) FROM unnest(tsvector_to_array(to_tsvector('simple',$2))) v),''::tsquery) AS prefix
 ), candidates AS (
 SELECT t.id,1000000 AS score,'reference'::text AS source,NULL::uuid AS comment_id FROM tasks t
 WHERE t.workspace_id=ANY($1::uuid[]) AND ($8 OR t.archived_at IS NULL) AND (t.id=NULLIF($7,'')::uuid OR (t.number=$4 AND EXISTS(SELECT 1 FROM task_prefixes p WHERE p.workspace_id=t.workspace_id AND p.prefix=$3)))
 UNION ALL
 SELECT t.id,CASE WHEN lower(t.title)=lower($2) THEN 900000 WHEN (t.search_title@@q.title OR to_tsvector('english',t.title)@@q.full) THEN 500000 WHEN t.search_title@@q.prefix THEN 400000 ELSE 200000 END + floor(ts_rank_cd(t.search_document,q.full,32)*99999)::int,
 CASE WHEN (t.search_title@@q.title OR to_tsvector('english',t.title)@@q.full) OR t.search_title@@q.prefix THEN 'title' ELSE 'description' END,NULL::uuid
 FROM tasks t CROSS JOIN query q WHERE t.workspace_id=ANY($1::uuid[]) AND ($8 OR t.archived_at IS NULL) AND (t.search_document@@q.full OR t.search_title@@q.prefix)
 UNION ALL
 SELECT t.id,100000+floor(ts_rank_cd(c.search_document,q.full,32)*99999)::int,'comment',c.id
 FROM task_comments c JOIN tasks t ON t.id=c.task_id CROSS JOIN query q
 WHERE t.workspace_id=ANY($1::uuid[]) AND ($8 OR t.archived_at IS NULL) AND NOT c.deleted AND c.search_document@@q.full
 ), best AS (SELECT DISTINCT ON(id) * FROM candidates ORDER BY id,score DESC,comment_id NULLS FIRST),
 page AS (SELECT * FROM best WHERE $5='' OR score<$6 OR (score=$6 AND id::text>$5) ORDER BY score DESC,id LIMIT 26),
 ancestry AS (
 SELECT p.id AS child,t.parent_id AS id,1 AS depth FROM page p JOIN tasks t ON t.id=p.id WHERE t.parent_id IS NOT NULL
 UNION ALL SELECT a.child,t.parent_id,a.depth+1 FROM ancestry a JOIN tasks t ON t.id=a.id WHERE t.parent_id IS NOT NULL
 )
 SELECT (t.archived_at IS NOT NULL),t.id::text,cfg.prefix||'-'||t.number,t.title,s.id::text,s.name,s.board,w.id::text,w.slug,w.name,p.source,COALESCE(p.comment_id::text,''),p.score,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',a.id,'reference',cfg.prefix||'-'||parent.number,'title',parent.title) ORDER BY a.depth DESC) FROM ancestry a JOIN tasks parent ON parent.id=a.id WHERE a.child=t.id),'[]'::jsonb),
 ts_headline('english',replace(replace(CASE p.source WHEN 'comment' THEN cm.body WHEN 'description' THEN t.description ELSE t.title END,chr(57344),''),chr(57345),''),CASE WHEN p.source='title' THEN q.prefix ELSE q.full END,
 'StartSel='||chr(57344)||', StopSel='||chr(57345)||', MaxWords=32, MinWords=12, MaxFragments=1, FragmentDelimiter= … ')
 FROM page p JOIN tasks t ON t.id=p.id JOIN workspaces w ON w.id=t.workspace_id JOIN task_settings cfg ON cfg.workspace_id=w.id JOIN task_statuses s ON s.id=t.status_id LEFT JOIN task_comments cm ON cm.id=p.comment_id CROSS JOIN query q
 ORDER BY p.score DESC,p.id`, allowed, q.Query, prefix, number, p.ID, p.Score, id, q.IncludeArchived)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	scores := []int{}
	for rows.Next() {
		var r tasks.SearchResult
		var ancestors []byte
		var snippet string
		var score int
		if e = rows.Scan(&r.Archived, &r.ID, &r.Reference, &r.Title, &r.Status.ID, &r.Status.Name, &r.Status.Board, &r.WorkspaceID, &r.WorkspaceSlug, &r.WorkspaceName, &r.Source, &r.CommentID, &score, &ancestors, &snippet); e != nil {
			return out, e
		}
		if e = json.Unmarshal(ancestors, &r.Ancestors); e != nil {
			return out, e
		}
		r.Excerpt = searchExcerpt(snippet)
		out.Tasks = append(out.Tasks, r)
		scores = append(scores, score)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Tasks) > tasks.SearchPageSize {
		out.More = true
		out.Tasks = out.Tasks[:tasks.SearchPageSize]
		last := out.Tasks[len(out.Tasks)-1]
		out.Cursor = q.Next(scores[tasks.SearchPageSize-1], last.ID)
	}
	return out, nil
}

// Never expose ts_headline as trusted HTML. Delimiters become plain text spans.
func searchExcerpt(s string) []tasks.SearchPart {
	parts := []tasks.SearchPart{}
	match := false
	for len(s) > 0 {
		i := strings.IndexAny(s, "\ue000\ue001")
		if i < 0 {
			parts = append(parts, tasks.SearchPart{Text: s, Match: match})
			break
		}
		if i > 0 {
			parts = append(parts, tasks.SearchPart{Text: s[:i], Match: match})
		}
		match = strings.HasPrefix(s[i:], "\ue000")
		s = s[i+3:]
	}
	remaining := 600
	bounded := []tasks.SearchPart{}
	for _, part := range parts {
		runes := []rune(part.Text)
		if len(runes) > remaining {
			part.Text = string(runes[:remaining]) + "…"
			bounded = append(bounded, part)
			break
		}
		bounded = append(bounded, part)
		remaining -= len(runes)
		if remaining == 0 {
			break
		}
	}
	return bounded
}
