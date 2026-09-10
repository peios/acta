package integration

import (
	"acta2/internal/accounts"
	"acta2/internal/auth"
	apiclient "acta2/internal/client"
	"acta2/internal/config"
	"acta2/internal/documents"
	"acta2/internal/httpapi"
	"acta2/internal/tasks"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDocumentsVersionsPermissionsAndPagination(t *testing.T) {
	root := securityDatabase(t)
	_, f, w := workspaceOwner(t, root, "documents")
	ctx := t.Context()
	m := manager(f)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Document owner"})
	must(t, e)
	in := documents.Save{Task: task.Reference, Title: "Design", Filename: "design.md", Content: []byte("# Version one")}
	one, e := m.SaveDocument(ctx, f.token, in)
	must(t, e)
	counted, e := m.Task(ctx, f.token, task.ID)
	must(t, e)
	if counted.DocumentCount != 1 {
		t.Fatal("document count missing", counted.DocumentCount)
	}
	if one.Revision != 1 || !one.CanWrite || one.MediaType != "text/markdown; charset=utf-8" {
		t.Fatal(one)
	}
	in.ID = one.ID
	in.Revision = 1
	in.Content = []byte("# Version two")
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, e := m.SaveDocument(ctx, f.token, in); results <- e })
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, documents.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	v, b, e := m.DocumentFile(ctx, f.token, one.ID, 1)
	must(t, e)
	if string(b) != "# Version one" || v.FileID != one.FileID {
		t.Fatal("old bytes mutated")
	}
	v, b, e = m.DocumentFile(ctx, f.token, one.ID, 0)
	must(t, e)
	if string(b) != "# Version two" || v.Revision != 2 || v.FileID == one.FileID {
		t.Fatal("replacement missing")
	}
	reader, rf := permissionMember(t, root, "reader")
	joinWorkspace(t, f, w, reader.ID)
	d, e := manager(rf).Document(ctx, rf.token, one.ID)
	must(t, e)
	if d.CanWrite {
		t.Fatal("read-only membership gained document write")
	}
	_, _, e = manager(rf).DocumentFile(ctx, rf.token, one.ID, 1)
	must(t, e)
	if e = manager(rf).DeleteDocument(ctx, rf.token, one.ID, 2); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = manager(rf).SaveDocument(ctx, rf.token, documents.Save{Task: task.ID, Filename: "no.txt", Content: []byte("no")}); !errors.Is(e, auth.ErrForbidden) {
		t.Fatal(e)
	}
	_, outsider := permissionMember(t, root, "outsider")
	if _, _, e = manager(outsider).DocumentFile(ctx, outsider.token, one.ID, 1); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("private document exposed", e)
	}
	if e = m.DeleteDocument(ctx, f.token, one.ID, 1); !errors.Is(e, documents.ErrConflict) {
		t.Fatal(e)
	}
	// Exercise both cursors past a complete page, not just an empty-list response.
	for i := 2; i < 52; i++ {
		in.Revision = int64(i)
		_, e = m.SaveDocument(ctx, f.token, in)
		must(t, e)
	}
	h, e := m.DocumentVersions(ctx, f.token, one.ID, 0)
	must(t, e)
	if len(h.Versions) != 50 || h.Before != 3 || h.Versions[0].Revision != 52 {
		t.Fatal(h)
	}
	h, e = m.DocumentVersions(ctx, f.token, one.ID, h.Before)
	must(t, e)
	if len(h.Versions) != 2 || h.Before != 0 || h.Versions[1].Revision != 1 {
		t.Fatal(h)
	}
	for range 51 {
		_, e = m.SaveDocument(ctx, f.token, documents.Save{Task: task.ID, Filename: "reference.txt", Content: []byte("ok")})
		must(t, e)
	}
	page, e := m.Documents(ctx, f.token, task.ID, "")
	must(t, e)
	if len(page.Documents) != 50 || page.Cursor == "" {
		t.Fatal(page)
	}
	tail, e := m.Documents(ctx, f.token, task.ID, page.Cursor)
	must(t, e)
	if len(tail.Documents) != 2 || tail.Cursor != "" {
		t.Fatal(tail)
	}
	counted, e = m.Task(ctx, f.token, task.ID)
	must(t, e)
	if counted.DocumentCount != 52 {
		t.Fatal("versions inflated count", counted.DocumentCount)
	}
	must(t, m.DeleteDocument(ctx, f.token, one.ID, 52))
	counted, e = m.Task(ctx, f.token, task.ID)
	must(t, e)
	if counted.DocumentCount != 51 {
		t.Fatal("deletion did not update count", counted.DocumentCount)
	}
	if _, _, e = m.DocumentFile(ctx, f.token, one.ID, 1); !errors.Is(e, auth.ErrNotFound) {
		t.Fatal(e)
	}
	var n int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM document_files WHERE file_id=$1`, one.FileID).Scan(&n))
	if n != 0 {
		t.Fatal("file leaked on delete")
	}
	// Failed compare-and-swap must not create a phantom version or activity item.
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM activity_entries WHERE subject_id=$1 AND change::text LIKE '%document.%'`, task.ID).Scan(&n))
	if n != 104 {
		t.Fatal("document activity count", n)
	}
}

func TestDocumentsHTTPCLIAndMCP(t *testing.T) {
	f := securityDatabase(t)
	ctx := t.Context()
	m := manager(f)
	w, e := m.CreateWorkspace(ctx, f.token, "Documents interfaces", "document-interfaces", "")
	must(t, e)
	task, e := m.CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Document interfaces"})
	must(t, e)
	agent := agentAccount(t, f, "document-agent", nil)
	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	cfg, e := config.Parse(srv.URL, "")
	must(t, e)
	handler = httpapi.New(f.service, f.security, accounts.NewProfileService(f.store), m, cfg)
	credential := agentCLI(t, f, agent.ID)
	c := apiclient.New(srv.URL, credential)
	original := []byte{0, 1, 2, 255, 128, 10}
	input := filepath.Join(t.TempDir(), "binary.dat")
	must(t, os.WriteFile(input, original, 0600))
	var d documents.Document
	must(t, json.Unmarshal(runTaskCLI(t, srv.URL, credential, "", "document", "upload", task.Reference, input, "--title", "Binary sample"), &d))
	output := filepath.Join(t.TempDir(), "download.dat")
	runTaskCLI(t, srv.URL, credential, "", "document", "download", d.ID, "--output", output)
	actual, e := os.ReadFile(output)
	must(t, e)
	if !bytes.Equal(original, actual) {
		t.Fatal("CLI binary round trip")
	}
	// Hostile content is never served inline, regardless of its filename.
	hostile, e := c.UploadDocument(ctx, documents.Save{Task: task.ID, Filename: "image.png", Content: []byte("<html><script>alert(1)</script></html>")})
	must(t, e)
	req, e := http.NewRequest("GET", fmt.Sprintf("%s/api/documents/%s/versions/1/file", srv.URL, hostile.ID), nil)
	must(t, e)
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("Range", "bytes=0-5")
	response, e := http.DefaultClient.Do(req)
	must(t, e)
	data, e := io.ReadAll(response.Body)
	response.Body.Close()
	must(t, e)
	if response.StatusCode != 206 || string(data) != "<html>" || !strings.HasPrefix(response.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(response.Header.Get("Content-Security-Policy"), "sandbox") || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/html") {
		t.Fatal(response.Status, response.Header, string(data))
	}
	req.Header.Del("Authorization")
	response, e = http.DefaultClient.Do(req)
	must(t, e)
	response.Body.Close()
	if response.StatusCode == 200 || response.StatusCode == 206 {
		t.Fatal("unauthorized download")
	}
	// A browser cannot use multipart to bypass the Origin check.
	req, e = http.NewRequest("POST", srv.URL+"/api/tasks/"+task.ID+"/documents", strings.NewReader("bad"))
	must(t, e)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req.AddCookie(&http.Cookie{Name: "acta_session", Value: f.token})
	response, e = http.DefaultClient.Do(req)
	must(t, e)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("multipart CSRF", response.StatusCode)
	}
	connect := func(grants []string) *mcp.ClientSession {
		o := oauthFromAccount(t, f, srv.URL+"/mcp")
		redirect, e := f.security.ApproveOAuthTools(ctx, f.token, o.request, f.binding, true, srv.URL, agent.ID, grants)
		must(t, e)
		u, e := url.Parse(redirect)
		must(t, e)
		o.code = u.Query().Get("code")
		tokens := o.exchange(t)
		session, e := mcp.NewClient(&mcp.Implementation{Name: "Document test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{tokens.AccessToken}}}, nil)
		must(t, e)
		t.Cleanup(func() { session.Close() })
		return session
	}
	s := connect([]string{auth.MCPTasksRead, auth.MCPTasksWrite})
	call := func(name string, args map[string]any) json.RawMessage {
		t.Helper()
		result, e := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		must(t, e)
		if result.IsError {
			t.Fatalf("%s: %+v", name, result)
		}
		b, e := json.Marshal(result.StructuredContent)
		must(t, e)
		var env struct{ Data json.RawMessage }
		must(t, json.Unmarshal(b, &env))
		return env.Data
	}
	text := strings.Repeat("x", 32767) + "€tail"
	must(t, json.Unmarshal(call("document_save", map[string]any{"task": task.ID, "title": "Unicode", "filename": "unicode.txt", "revision": 0, "content": text}), &d))
	var chunk struct {
		Content    string
		NextOffset *int64 `json:"next_offset"`
		Revision   int64
	}
	must(t, json.Unmarshal(call("document_read", map[string]any{"id": d.ID, "revision": 0}), &chunk))
	if chunk.NextOffset == nil || *chunk.NextOffset != 32767 || chunk.Revision != 1 {
		t.Fatal(chunk)
	}
	first := chunk.Content
	must(t, json.Unmarshal(call("document_read", map[string]any{"id": d.ID, "revision": 1, "offset": *chunk.NextOffset}), &chunk))
	if first+chunk.Content != text || chunk.NextOffset != nil {
		t.Fatal("chunk boundary lost")
	}
	call("document_delete", map[string]any{"id": d.ID, "revision": 1})
	readonly := connect([]string{auth.MCPTasksRead})
	available, e := readonly.ListTools(ctx, nil)
	must(t, e)
	count := 0
	for _, tool := range available.Tools {
		if strings.HasPrefix(tool.Name, "document") {
			count++
		}
		if tool.Name == "document_save" || tool.Name == "document_delete" {
			t.Fatal("read grant gained writes")
		}
	}
	if count != 4 {
		t.Fatal("read tools missing", count)
	}
}
