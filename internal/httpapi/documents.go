package httpapi

import (
	"acta2/internal/documents"
	"bytes"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func documentUpload(r *http.Request) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return r.Method == "POST" && len(parts) == 4 && parts[0] == "api" && parts[1] == "tasks" && parts[2] != "" && parts[3] == "documents"
}
func documentNumber(value, field string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	v, e := strconv.ParseInt(value, 10, 64)
	if e != nil || v < 0 {
		return 0, documents.Invalid(field, "Supply a non-negative revision.")
	}
	return v, nil
}
func (h *Handler) documentRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/tasks/{task}/documents", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.Documents(r.Context(), token(r, h.sessionCookie), r.PathValue("task"), r.URL.Query().Get("cursor"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("POST /api/tasks/{task}/documents", func(w http.ResponseWriter, r *http.Request) {
		// Bound the complete multipart request, including arbitrary extra fields.
		r.Body = http.MaxBytesReader(w, r.Body, documents.MaxBytes+64*1024)
		if e := r.ParseMultipartForm(1024 * 1024); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			failure(w, documents.Invalid("file", "Supply a file of up to 20 MiB."))
			return
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
			failure(w, documents.Invalid("file", "Upload one file per document version."))
			return
		}
		f, header, e := r.FormFile("file")
		if e != nil {
			failure(w, documents.Invalid("file", "Choose a file."))
			return
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, documents.MaxBytes+1))
		if e != nil {
			failure(w, e)
			return
		}
		rev, e := documentNumber(r.FormValue("revision"), "revision")
		if e != nil {
			failure(w, e)
			return
		}
		v, e := h.management.SaveDocument(r.Context(), token(r, h.sessionCookie), documents.Save{ID: r.FormValue("id"), Task: r.PathValue("task"), Title: r.FormValue("title"), Filename: header.Filename, Revision: rev, Content: b})
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/documents/{document}", func(w http.ResponseWriter, r *http.Request) {
		v, e := h.management.Document(r.Context(), token(r, h.sessionCookie), r.PathValue("document"))
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/documents/{document}/versions", func(w http.ResponseWriter, r *http.Request) {
		before, e := documentNumber(r.URL.Query().Get("before"), "before")
		if e != nil {
			failure(w, e)
			return
		}
		v, e := h.management.DocumentVersions(r.Context(), token(r, h.sessionCookie), r.PathValue("document"), before)
		if e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, v)
	})
	m.HandleFunc("GET /api/documents/{document}/versions/{revision}/file", func(w http.ResponseWriter, r *http.Request) {
		rev, e := documentNumber(r.PathValue("revision"), "revision")
		if e != nil || rev == 0 {
			failure(w, documents.Invalid("revision", "Download an explicit positive revision."))
			return
		}
		v, b, e := h.management.DocumentFile(r.Context(), token(r, h.sessionCookie), r.PathValue("document"), rev)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("Content-Type", v.MediaType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Filename}))
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		http.ServeContent(w, r, v.Filename, v.CreatedAt, bytes.NewReader(b))
	})
	m.HandleFunc("POST /api/documents/{document}/delete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int64 `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := h.management.DeleteDocument(r.Context(), token(r, h.sessionCookie), r.PathValue("document"), in.Revision); e != nil {
			failure(w, e)
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
}
