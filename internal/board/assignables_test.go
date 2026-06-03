package board_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/peios/acta/internal/board"
	"github.com/peios/acta/internal/store"
	"github.com/peios/acta/internal/store/memstore"
)

func TestAssignables(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	me, _ := ms.CreateUser(ctx, store.NewUser{Username: "ada", Display: "Ada"})
	other, _ := ms.CreateUser(ctx, store.NewUser{Username: "ben", Display: "Ben"})
	_, _ = ms.CreateUser(ctx, store.NewUser{Username: "ada/bot", Display: "Ada Bot", AgentOfID: me.ID})
	_, _ = ms.CreateUser(ctx, store.NewUser{Username: "ben/bot", Display: "Ben Bot", AgentOfID: other.ID})
	gone, _ := ms.CreateUser(ctx, store.NewUser{Username: "zed", Display: "Zed"})
	if err := ms.SetUserDisabled(ctx, gone.ID, true); err != nil {
		t.Fatal(err)
	}
	svc := board.New(ms)

	// As Ada: active humans (alpha by display), then her own agent. Ben's agent
	// and the disabled human Zed are excluded.
	got, err := svc.Assignables(actorCtx(me.ID, "Ada"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ada", "ben", "ada/bot"}; !reflect.DeepEqual(usernames(got), want) {
		t.Fatalf("Assignables(Ada) = %v, want %v", usernames(got), want)
	}

	// With no principal in context there are no "my agents" — humans only.
	anon, err := svc.Assignables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ada", "ben"}; !reflect.DeepEqual(usernames(anon), want) {
		t.Fatalf("Assignables(anon) = %v, want %v", usernames(anon), want)
	}
}

func usernames(us []store.User) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Username
	}
	return out
}
