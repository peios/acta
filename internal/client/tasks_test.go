package client

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"acta/internal/tasks"
)

type taskTransport func(*http.Request) (*http.Response, error)

func (f taskTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTaskQueryEncodingAndIntegerPrecision(t *testing.T) {
	c := New("http://localhost:8081", "cli_fixture")
	c.HTTP.Transport = taskTransport(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if r.URL.Path != "/api/workspaces/example/tasks" || q.Get("summary") != "true" || q.Get("parent") != "ACT-12" || q.Get("q") != "a & b" || q.Get("cursor") != "a+b/c=" || q.Get("unassigned") != "true" || !reflect.DeepEqual(q["status"], []string{"first", "second"}) || !reflect.DeepEqual(q["assignee"], []string{"user1", "user2"}) {
			t.Fatal(r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer cli_fixture" {
			t.Fatal("missing bearer")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tasks":[],"total":9007199254740993,"more":false,"cursor":""}`)), Header: make(http.Header)}, nil
	})
	page, err := c.Tasks(t.Context(), "example", tasks.Filter{Parent: "ACT-12", Query: "a & b", Cursor: "a+b/c=", Statuses: []string{"first", "second"}, Assignees: []string{"user1", "user2"}, Unassigned: true})
	if err != nil || page.Total != 9007199254740993 {
		t.Fatal(page, err)
	}
}

func TestClientPreservesConflictAndFieldErrors(t *testing.T) {
	c := New("http://localhost:8081", "")
	c.HTTP.Transport = taskTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"conflict","message":"Changed","fields":{"title":"Changed"},"current":{"field":"title","version":9007199254740993,"value":"Current"}}}`)), Header: make(http.Header)}, nil
	})
	_, err := c.UpdateTask(t.Context(), "ACT-1", tasks.Patch{})
	var problem *Error
	if !errors.As(err, &problem) || problem.Status != 409 || problem.Code != "conflict" || problem.Fields["title"] != "Changed" || !strings.Contains(string(problem.Current), "9007199254740993") {
		t.Fatal(err)
	}
}
