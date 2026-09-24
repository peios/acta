package codehost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"acta/internal/client"
	"acta/internal/codehosts"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

func TestRunRetriesAndServesCatalogue(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var attempts atomic.Int32
	served := make(chan bool, 1)
	in := codehosts.Heartbeat{ID: uuid.NewString(), InstanceID: uuid.NewString(), Name: "desktop", OS: "linux", Arch: "amd64"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cli_test" {
			t.Error("missing shared credential")
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(503)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		var hello codehosts.Heartbeat
		if err = wsjson.Read(ctx, conn, &hello); err != nil {
			t.Error(err)
			return
		}
		if hello.ID != in.ID || hello.Name != in.Name {
			t.Error("retry changed host")
		}
		if err = wsjson.Write(ctx, conn, codehosts.Response{Result: json.RawMessage(`{"ready":true}`)}); err != nil {
			t.Error(err)
			return
		}
		if err = wsjson.Write(ctx, conn, codehosts.Request{ID: "request", Method: "codebases.list", Params: json.RawMessage(`{}`)}); err != nil {
			t.Error(err)
			return
		}
		var result codehosts.Response
		for result.ID != "request" {
			if err = wsjson.Read(ctx, conn, &result); err != nil {
				t.Error(err)
				return
			}
		}
		served <- result.ID == "request" && string(result.Result) == `{"codebases":[]}`
		cancel()
	}))
	defer server.Close()
	catalogue, err := OpenCatalogue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = Run(ctx, client.New(server.URL, "cli_test"), in, catalogue, io.Discard); err != nil {
		t.Fatal(err)
	}
	select {
	case good := <-served:
		if !good {
			t.Fatal("wrong response")
		}
	default:
		t.Fatal("request never served")
	}
	if attempts.Load() != 2 {
		t.Fatal("retry count", attempts.Load())
	}
}

func TestRunStopsOnRejectedCredential(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); w.WriteHeader(401) }))
	defer server.Close()
	catalogue, err := OpenCatalogue(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := codehosts.Heartbeat{ID: uuid.NewString(), InstanceID: uuid.NewString(), Name: "desktop", OS: "linux", Arch: "amd64"}
	if err = Run(t.Context(), client.New(server.URL, "cli_test"), in, catalogue, io.Discard); err == nil {
		t.Fatal("credential rejection ignored")
	}
	if attempts.Load() != 1 {
		t.Fatal("rejected credential retried")
	}
}
