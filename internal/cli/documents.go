package cli

import (
	"acta2/internal/client"
	"acta2/internal/documents"
	"context"
	"encoding/json"
	"errors"
	"github.com/spf13/cobra"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

func renderDocumentJSON[T any](v T) string {
	raw, _ := json.MarshalIndent(v, "", "  ")
	return string(raw)
}
func (a *App) documentCommand() *cobra.Command {
	root := &cobra.Command{Use: "document", Short: "Read and manage task documents"}
	var cursor string
	list := &cobra.Command{Use: "list <task>", Args: cobra.ExactArgs(1), Short: "List task documents", RunE: func(cmd *cobra.Command, args []string) error {
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (documents.Page, error) {
			var out documents.Page
			e := c.Call(ctx, "GET", "tasks/"+url.PathEscape(args[0])+"/documents?cursor="+url.QueryEscape(cursor), nil, &out)
			return out, e
		}, renderDocumentJSON[documents.Page])
	}}
	list.Flags().StringVar(&cursor, "cursor", "", "Continue from the returned cursor")
	var in documents.Save
	upload := &cobra.Command{Use: "upload <task> <file>", Args: cobra.ExactArgs(2), Short: "Upload a document or a new version", RunE: func(cmd *cobra.Command, args []string) error {
		f, e := os.Open(args[1])
		if e != nil {
			return e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, documents.MaxBytes+1))
		if e != nil {
			return e
		}
		input := in
		input.Task = args[0]
		input.Filename = filepath.Base(args[1])
		input.Content = b
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (documents.Document, error) {
			return c.UploadDocument(ctx, input)
		}, renderDocumentJSON[documents.Document])
	}}
	upload.Flags().StringVar(&in.ID, "id", "", "Document UUID; required with --revision for an update")
	upload.Flags().Int64Var(&in.Revision, "revision", 0, "Latest revision read; 0 creates a document")
	upload.Flags().StringVar(&in.Title, "title", "", "Document title; defaults to filename")
	var rev int64
	var output string
	download := &cobra.Command{Use: "download <id>", Args: cobra.ExactArgs(1), Short: "Download an immutable version to a new local file", RunE: func(cmd *cobra.Command, args []string) error {
		if output == "" {
			return errors.New("Supply --output; existing files are never overwritten")
		}
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (map[string]any, error) {
			current := rev
			if current == 0 {
				var d documents.Document
				if e := c.Call(ctx, "GET", "documents/"+url.PathEscape(args[0]), nil, &d); e != nil {
					return nil, e
				}
				current = d.Revision
			}
			f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return nil, e
			}
			e = c.DownloadDocument(ctx, args[0], current, f)
			closeError := f.Close()
			if e == nil {
				e = closeError
			}
			if e != nil {
				os.Remove(output)
				return nil, e
			}
			return map[string]any{"path": output, "revision": current}, nil
		}, renderDocumentJSON[map[string]any])
	}}
	download.Flags().Int64Var(&rev, "revision", 0, "Version to download; 0 selects the latest")
	download.Flags().StringVar(&output, "output", "", "Destination path (must not exist)")
	root.AddCommand(list, upload, download)
	return root
}
