package web_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestBoardListLayout checks the Board⇄List display toggle: the board container
// carries data-layout, the Display-menu segment reflects the active layout, and
// ?layout=list is threaded through the filter form so applying a filter keeps it.
func TestBoardListLayout(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)
	todo := statusID(t, client, base, "To do")
	createItem(t, client, base, token, todo, "a task")

	// Default: the board layout, Board segment active.
	board := getBody(t, client, base+"/general", http.StatusOK)
	if !strings.Contains(board, `data-layout="board"`) {
		t.Error(`default board should render data-layout="board"`)
	}
	if !strings.Contains(board, `class="seg-opt active" data-layout-opt="board"`) {
		t.Error("the Board segment should be active by default")
	}

	// ?layout=list flips the container and the active segment.
	list := getBody(t, client, base+"/general?layout=list", http.StatusOK)
	if !strings.Contains(list, `data-layout="list"`) {
		t.Error(`?layout=list should render data-layout="list"`)
	}
	if !strings.Contains(list, `class="seg-opt active" data-layout-opt="list"`) {
		t.Error("the List segment should be active under ?layout=list")
	}
	// The filter form carries layout so applying/clearing a filter keeps the lens.
	if !strings.Contains(list, `<input type="hidden" name="layout" value="list">`) {
		t.Error("the filter form should carry the list layout through")
	}
}

// TestBoardListLayoutComposesWithMode confirms layout is orthogonal to grouping:
// list layout holds while the board is grouped by milestone.
func TestBoardListLayoutComposesWithMode(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general?layout=list&mode=milestone", http.StatusOK)
	if !strings.Contains(body, `data-mode="milestone"`) || !strings.Contains(body, `data-layout="list"`) {
		t.Error("list layout should compose with milestone grouping")
	}
}

// TestBoardLayoutDefaultsToBoard checks an unknown layout value falls back to the
// column board rather than erroring or leaking through.
func TestBoardLayoutDefaultsToBoard(t *testing.T) {
	base, client := newTestServer(t)
	token := csrfToken(t, client, base)
	login(t, client, base, token)

	body := getBody(t, client, base+"/general?layout=bogus", http.StatusOK)
	if !strings.Contains(body, `data-layout="board"`) {
		t.Error("an unknown layout should fall back to board")
	}
	if strings.Contains(body, `data-layout="bogus"`) {
		t.Error("an unknown layout value must not be reflected into the DOM")
	}
}
