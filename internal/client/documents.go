package client

import (
	"acta2/internal/documents"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (c *Client) documentRequest(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	server, e := NormalizeURL(c.URL)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, method, server+"/api/"+path, body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "acta2-cli")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	transferClient := *c.HTTP
	transferClient.Timeout = 2 * time.Minute
	resp, e := transferClient.Do(req)
	if e != nil {
		return nil, e
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var v struct{ Error Error }
		json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&v)
		v.Error.Status = resp.StatusCode
		if v.Error.Message == "" {
			v.Error.Message = fmt.Sprintf("Acta returned HTTP %d", resp.StatusCode)
		}
		return nil, &v.Error
	}
	return resp, nil
}
func (c *Client) UploadDocument(ctx context.Context, in documents.Save) (documents.Document, error) {
	var out documents.Document
	if e := documents.Validate(&in); e != nil {
		return out, e
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range map[string]string{"id": in.ID, "title": in.Title, "revision": strconv.FormatInt(in.Revision, 10)} {
		if e := w.WriteField(k, v); e != nil {
			return out, e
		}
	}
	f, e := w.CreateFormFile("file", in.Filename)
	if e != nil {
		return out, e
	}
	if _, e = f.Write(in.Content); e != nil {
		return out, e
	}
	if e = w.Close(); e != nil {
		return out, e
	}
	resp, e := c.documentRequest(ctx, "POST", "tasks/"+url.PathEscape(in.Task)+"/documents", w.FormDataContentType(), &body)
	if e != nil {
		return out, e
	}
	defer resp.Body.Close()
	e = json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&out)
	return out, e
}
func (c *Client) DownloadDocument(ctx context.Context, id string, revision int64, dst io.Writer) error {
	resp, e := c.documentRequest(ctx, "GET", "documents/"+url.PathEscape(id)+"/versions/"+strconv.FormatInt(revision, 10)+"/file", "", nil)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	n, e := io.Copy(dst, io.LimitReader(resp.Body, documents.MaxBytes+1))
	if e != nil {
		return e
	}
	if n > documents.MaxBytes {
		return fmt.Errorf("document exceeds 20 MiB")
	}
	return nil
}
