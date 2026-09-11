package client

import (
	"acta/internal/activity"
	"acta/internal/comments"
	"context"
	"net/url"
)

func (c *Client) Comment(ctx context.Context, task, id string) (activity.Entry, error) {
	var out activity.Entry
	e := c.Call(ctx, "GET", "tasks/"+url.PathEscape(task)+"/comments/"+url.PathEscape(id), nil, &out)
	return out, e
}
func (c *Client) CommentReplies(ctx context.Context, task, id, cursor string) (activity.Page, error) {
	var out activity.Page
	e := c.Call(ctx, "GET", "tasks/"+url.PathEscape(task)+"/comments/"+url.PathEscape(id)+"/replies?cursor="+url.QueryEscape(cursor), nil, &out)
	return out, e
}
func (c *Client) CreateComment(ctx context.Context, task string, in comments.Create) (activity.Entry, error) {
	var out activity.Entry
	e := c.Call(ctx, "POST", "tasks/"+url.PathEscape(task)+"/comments", in, &out)
	return out, e
}
func (c *Client) UpdateComment(ctx context.Context, task, id string, in comments.Update) (activity.Entry, error) {
	var out activity.Entry
	e := c.Call(ctx, "POST", "tasks/"+url.PathEscape(task)+"/comments/"+url.PathEscape(id), in, &out)
	return out, e
}
