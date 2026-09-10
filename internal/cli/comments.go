package cli

import (
	"acta2/internal/activity"
	"acta2/internal/client"
	"acta2/internal/comments"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func renderComment(e activity.Entry) string {
	return renderActivity(activity.Page{Entries: []activity.Entry{e}})
}
func (a *App) commentCommand() *cobra.Command {
	root := &cobra.Command{Use: "comment", Short: "Post, read, edit and delete task comments and replies"}
	get := &cobra.Command{Use: "get <task> <comment-uuid>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (activity.Entry, error) {
			return c.Comment(ctx, args[0], args[1])
		}, renderComment)
	}}
	var cursor string
	replies := &cobra.Command{Use: "replies <task> <root-comment-uuid>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return runTask(a, cmd, func(ctx context.Context, c *client.Client) (activity.Page, error) {
			return c.CommentReplies(ctx, args[0], args[1], cursor)
		}, renderActivity)
	}}
	replies.Flags().StringVar(&cursor, "cursor", "", "Older replies cursor")
	for _, mode := range []string{"add", "edit", "delete"} {
		mode := mode
		var body, file, reply, key string
		var version int64
		count := 2
		if mode == "add" {
			count = 1
		}
		cmd := &cobra.Command{Use: mode + " <task>", Args: cobra.ExactArgs(count), RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "delete" {
				if cmd.Flags().Changed("body") == cmd.Flags().Changed("body-file") {
					return errors.New("Supply exactly one of --body or --body-file (use - for stdin)")
				}
				if file != "" {
					var e error
					body, e = a.readTaskText(file)
					if e != nil {
						return e
					}
				}
			}
			if mode == "add" {
				if key == "" {
					key = uuid.NewString()
					fmt.Fprintln(a.errOut, "Post request ID:", key)
				}
				return runTask(a, cmd, func(ctx context.Context, c *client.Client) (activity.Entry, error) {
					return c.CreateComment(ctx, args[0], comments.Create{Body: body, ReplyTo: reply, RequestID: key})
				}, renderComment)
			}
			if version < 1 {
				return errors.New("Supply --version from comment get")
			}
			return runTask(a, cmd, func(ctx context.Context, c *client.Client) (activity.Entry, error) {
				return c.UpdateComment(ctx, args[0], args[1], comments.Update{Body: body, Version: version, Delete: mode == "delete"})
			}, renderComment)
		}}
		if mode != "add" {
			cmd.Use = mode + " <task> <comment-uuid>"
			cmd.Flags().Int64Var(&version, "version", 0, "Expected comment version")
		}
		if mode != "delete" {
			cmd.Flags().StringVar(&body, "body", "", "Markdown text")
			cmd.Flags().StringVar(&file, "body-file", "", "Markdown file, or - for stdin")
		}
		if mode == "add" {
			cmd.Flags().StringVar(&reply, "reply-to", "", "Comment UUID to reply to")
			cmd.Flags().StringVar(&key, "request-id", "", "Retry UUID (generated and printed if omitted)")
		}
		root.AddCommand(cmd)
	}
	root.AddCommand(get, replies)
	return root
}
