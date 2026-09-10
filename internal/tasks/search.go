package tasks

import (
	"acta2/internal/accounts"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"unicode/utf8"
)

const SearchPageSize = 25

type SearchQuery struct {
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"Include archived tasks alongside active tasks; default false."`
	Query           string `json:"query" jsonschema:"Words to find in task titles, descriptions and comments, or an exact task UUID/reference. Title word prefixes also match."`
	Workspace       string `json:"workspace,omitempty" jsonschema:"Optional workspace UUID or slug; omit to search all accessible workspaces."`
	Cursor          string `json:"cursor,omitempty" jsonschema:"Returned cursor for more results. Keep query and workspace unchanged."`
}
type SearchPosition struct {
	Fingerprint string `json:"q"`
	Score       int    `json:"s"`
	ID          string `json:"id"`
}
type SearchPart struct {
	Text  string `json:"text"`
	Match bool   `json:"match,omitempty"`
}
type SearchAncestor struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Title     string `json:"title"`
}
type SearchResult struct {
	Archived      bool             `json:"archived"`
	ID            string           `json:"id"`
	Reference     string           `json:"reference"`
	Title         string           `json:"title"`
	Status        Status           `json:"status"`
	WorkspaceID   string           `json:"workspace_id"`
	WorkspaceSlug string           `json:"workspace_slug"`
	WorkspaceName string           `json:"workspace_name"`
	Ancestors     []SearchAncestor `json:"ancestors"`
	Source        string           `json:"source"`
	CommentID     string           `json:"comment_id,omitempty"`
	Excerpt       []SearchPart     `json:"excerpt"`
}
type SearchPage struct {
	Tasks  []SearchResult `json:"tasks"`
	More   bool           `json:"more"`
	Cursor string         `json:"cursor"`
}

func (q SearchQuery) Fingerprint() string {
	h := sha256.Sum256([]byte(q.Query + "\x00" + q.Workspace + "\x00" + strconv.FormatBool(q.IncludeArchived)))
	return hex.EncodeToString(h[:])
}
func NormalizeSearch(q SearchQuery) (SearchQuery, SearchPosition, error) {
	q.Query = strings.TrimSpace(q.Query)
	q.Workspace = strings.ToLower(strings.TrimSpace(q.Workspace))
	bad := func(field, message string) (SearchQuery, SearchPosition, error) {
		return q, SearchPosition{}, &accounts.FieldError{Field: field, Message: message}
	}
	if !utf8.ValidString(q.Query) || utf8.RuneCountInString(q.Query) < 2 || utf8.RuneCountInString(q.Query) > 200 || strings.ContainsRune(q.Query, 0) {
		return bad("query", "Use between 2 and 200 characters.")
	}
	var p SearchPosition
	if q.Cursor != "" {
		if len(q.Cursor) > 1024 {
			return bad("cursor", "Invalid search cursor.")
		}
		b, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil || json.Unmarshal(b, &p) != nil {
			return bad("cursor", "Invalid search cursor.")
		}
		if _, e = uuid.Parse(p.ID); e != nil || p.Score < 0 || p.Score > 1000000 || p.Fingerprint != q.Fingerprint() {
			return bad("cursor", "Keep the search query and workspace unchanged while paging.")
		}
	}
	return q, p, nil
}
func (q SearchQuery) Next(score int, id string) string {
	b, _ := json.Marshal(SearchPosition{q.Fingerprint(), score, id})
	return base64.RawURLEncoding.EncodeToString(b)
}
