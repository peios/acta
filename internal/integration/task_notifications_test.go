package integration

import (
	"acta/internal/activity"
	"acta/internal/auth"
	"acta/internal/comments"
	"acta/internal/config"
	"acta/internal/httpapi"
	"acta/internal/push"
	"acta/internal/tasks"
	"acta/internal/threads"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func taskNotices(t *testing.T, f securityFixture, owner string, want int) threads.NotificationPage {
	t.Helper()
	p, e := f.store.ThreadNotifications(t.Context(), owner)
	must(t, e)
	if p.Total != want || len(p.Items) != want {
		t.Fatalf("want %d notices, got %+v", want, p)
	}
	return p
}
func TestTaskNotificationsFollowingAssignmentsAndAgents(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "tasknotices")
	ctx := t.Context()
	admin, e := root.service.Current(ctx, root.token)
	must(t, e)
	task, e := manager(f).CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Notify"})
	must(t, e)
	follow, e := manager(f).TaskFollowing(ctx, f.token, task.ID)
	must(t, e)
	if !follow {
		t.Fatal("creator not followed")
	}
	taskNotices(t, f, owner.ID, 0)
	task, e = patchTask(t, root, task, "title", "Renamed")
	must(t, e)
	p := taskNotices(t, f, owner.ID, 1)
	n := p.Items[0]
	if n.TaskID != task.ID || n.TaskReference != task.Reference || n.WorkspaceSlug != w.Slug || n.TaskTitle != "Renamed" {
		t.Fatal(n)
	}
	task, e = patchTask(t, root, task, "title", "Final")
	must(t, e)
	p = taskNotices(t, f, owner.ID, 1)
	if p.Items[0].ID != n.ID || p.Items[0].Revision <= n.Revision {
		t.Fatal("group not coalesced", p)
	}
	must(t, f.store.ReadThreadNotifications(ctx, owner.ID, []threads.NotificationRead{{ID: n.ID, Revision: n.Revision}}))
	taskNotices(t, f, owner.ID, 1)
	entry := history(t, f, task.ID).Entries[0]
	must(t, manager(f).ReadTaskActivity(ctx, f.token, task.ID, []activity.Seen{{ID: entry.ID, Through: entry.Last}}))
	taskNotices(t, f, owner.ID, 0)
	must(t, manager(f).SetTaskFollowing(ctx, f.token, task.ID, false))
	task, e = patchTask(t, root, task, "priority", "high")
	must(t, e)
	taskNotices(t, f, owner.ID, 0)
	agent := agentAccount(t, f, "helper", nil)
	af := f
	af.token = agentCLI(t, f, agent.ID)
	task, e = patchTask(t, root, task, "assignees", []string{owner.ID, agent.ID})
	must(t, e)
	taskNotices(t, f, owner.ID, 1) // direct assignment still notifies, owner deduplicated
	follow, e = manager(f).TaskFollowing(ctx, f.token, task.ID)
	must(t, e)
	if follow {
		t.Fatal("explicit unfollow overridden")
	}
	_, e = manager(af).TaskFollowing(ctx, af.token, task.ID)
	if !errors.Is(e, auth.ErrForbidden) {
		t.Fatal("agent may not change owner's preference", e)
	}
	created, e := manager(af).CreateTask(ctx, af.token, w.ID, tasks.Create{Title: "Agent-created"})
	must(t, e)
	follow, e = manager(f).TaskFollowing(ctx, f.token, created.ID)
	must(t, e)
	if !follow {
		t.Fatal("agent creator not resolved to owner")
	}
	p = taskNotices(t, f, owner.ID, 2)
	if !strings.Contains(p.Items[0].Title, "helper") {
		t.Fatal("agent identity lost", p)
	}
	// A task created by someone else follows new human and agent assignees once.
	other, e := manager(root).CreateTask(ctx, root.token, w.ID, tasks.Create{Title: "Assigned", Assignees: []string{agent.ID, owner.ID}})
	must(t, e)
	follow, e = manager(f).TaskFollowing(ctx, f.token, other.ID)
	must(t, e)
	if !follow {
		t.Fatal("assignment not followed")
	}
	taskNotices(t, f, owner.ID, 3)
	other, e = patchTask(t, root, other, "assignees", []string{admin.ID})
	must(t, e)
	follow, e = manager(f).TaskFollowing(ctx, f.token, other.ID)
	must(t, e)
	if !follow {
		t.Fatal("unassignment erased follow")
	}
}
func TestTaskNotificationsMentionsRepliesAccessAndReplay(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "mentions")
	ctx := t.Context()
	outsider, foreign := permissionMember(t, root, "outsider")
	task, e := manager(root).CreateTask(ctx, root.token, w.ID, tasks.Create{Title: "Discussion"})
	must(t, e)
	in := comments.Create{RequestID: uuid.NewString(), Body: "Hi @" + owner.Username + " and @" + owner.Username + ". `@outsider`"}
	c, e := manager(root).CreateComment(ctx, root.token, task.ID, in)
	must(t, e)
	_, e = manager(root).CreateComment(ctx, root.token, task.ID, in)
	must(t, e)
	p := taskNotices(t, f, owner.ID, 1)
	if !strings.Contains(p.Items[0].Title, "Mentioned") {
		t.Fatal(p)
	}
	taskNotices(t, foreign, outsider.ID, 0)
	c, e = manager(root).UpdateComment(ctx, root.token, task.ID, c.ID, comments.Update{Version: c.Comment.Version, Body: in.Body + " More prose."})
	must(t, e)
	if next := taskNotices(t, f, owner.ID, 1).Items[0]; next.Revision != p.Items[0].Revision {
		t.Fatal("unchanged mention re-pinged")
	}
	must(t, f.store.ReadThreadNotifications(ctx, owner.ID, []threads.NotificationRead{{ID: p.Items[0].ID, Revision: p.Items[0].Revision}}))
	// Replies notify the comment author even when they are not following.
	parent := postComment(t, f, task.ID, "Question", "")
	postComment(t, root, task.ID, "Answer", parent.ID)
	p = taskNotices(t, f, owner.ID, 1)
	if !strings.Contains(p.Items[0].Title, "Replied") {
		t.Fatal(p)
	}
	// A foreign owner cannot acknowledge the notice, and revocation hides it.
	must(t, f.store.ReadThreadNotifications(ctx, outsider.ID, []threads.NotificationRead{{ID: p.Items[0].ID, Revision: p.Items[0].Revision}}))
	taskNotices(t, f, owner.ID, 1)
	w = readWorkspace(t, root, w.ID)
	must(t, manager(root).WorkspaceMembership(ctx, root.token, w.ID, owner.ID, w.Version, false))
	taskNotices(t, f, owner.ID, 0)
	_, e = manager(f).TaskFollowing(ctx, f.token, task.ID)
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("follow leaked task", e)
	}
}
func TestTaskNotificationsRollbackAndReadRace(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "atomicnotices")
	ctx := t.Context()
	task, e := manager(f).CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Atomic"})
	must(t, e)
	admin, e := root.service.Current(ctx, root.token)
	must(t, e)
	sentinel := errors.New("abort")
	e = root.store.WithWorkspaces(ctx, admin.ID, auth.Digest(root.token), time.Now(), true, func(tx auth.WorkspaceTx) error {
		_, err := tx.TaskPatch(ctx, task, tasks.Patch{Field: "title", Version: task.Versions["title"], Value: []byte(`"Rolled back"`)})
		if err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	taskNotices(t, f, owner.ID, 0)
	task, e = patchTask(t, root, task, "title", "One")
	must(t, e)
	p := taskNotices(t, f, owner.ID, 1)
	n := p.Items[0]
	task, e = patchTask(t, root, task, "title", "Two")
	must(t, e)
	must(t, manager(f).ReadTaskActivity(ctx, f.token, task.ID, []activity.Seen{{ID: n.ActivityID, Through: strconv.FormatInt(n.Sequence, 10)}}))
	taskNotices(t, f, owner.ID, 1)
}

func TestTaskNotificationsPushAndFollowingHTTP(t *testing.T) {
	root := securityDatabase(t)
	owner, f, w := workspaceOwner(t, root, "taskpush")
	ctx := t.Context()
	task, e := manager(f).CreateTask(ctx, f.token, w.ID, tasks.Create{Title: "Private task"})
	must(t, e)
	cfg, e := config.Parse("http://localhost:8081", "")
	must(t, e)
	c := client{t, httpapi.New(f.service, f.security, nil, manager(f), cfg, "key"), map[string]*http.Cookie{"acta_session": {Name: "acta_session", Value: f.token}}}
	c.request("GET", "tasks/"+task.ID+"/following", "", 200)
	c.request("POST", "tasks/"+task.ID+"/following", `{}`, 400)
	c.request("POST", "tasks/"+task.ID+"/following", `{"following":false}`, 200)
	c.request("POST", "tasks/"+task.ID+"/following", `{"following":true}`, 200)
	receiver, e := ecdh.P256().GenerateKey(rand.Reader)
	must(t, e)
	sub := push.Subscription{Endpoint: "https://push.example/task", Keys: webpush.Keys{Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16)), P256dh: base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())}}
	subscription, e := f.store.SavePushSubscription(ctx, owner.ID, auth.Digest(f.token), sub)
	must(t, e)
	postComment(t, root, task.ID, "Private comment", "")
	p := taskNotices(t, f, owner.ID, 1)
	n := p.Items[0]
	var deliveries int
	must(t, f.conn.QueryRow(ctx, `SELECT count(*) FROM push_deliveries WHERE notification_id=$1`, n.ID).Scan(&deliveries))
	if deliveries != 1 {
		t.Fatal(deliveries)
	}
	got, e := f.store.PushNotice(ctx, owner.ID, auth.Digest(f.token), subscription, n.ID, n.Revision)
	must(t, e)
	if got.TaskTitle != task.Title || got.WorkspaceSlug != w.Slug {
		t.Fatal(got)
	}
	c.request("GET", "notifications", "", 200)
	w = readWorkspace(t, root, w.ID)
	must(t, manager(root).WorkspaceMembership(ctx, root.token, w.ID, owner.ID, w.Version, false))
	_, e = f.store.PushNotice(ctx, owner.ID, auth.Digest(f.token), subscription, n.ID, n.Revision)
	if !errors.Is(e, auth.ErrNotFound) {
		t.Fatal("push leaked revoked task", e)
	}
	c.request("GET", "tasks/"+task.ID+"/following", "", 404)
	c.request("POST", "tasks/"+task.ID+"/following", `{"following":true}`, 404)
}
