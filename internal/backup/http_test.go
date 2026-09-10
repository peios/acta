package backup

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalAPIAuthAndSocketOwnership(t *testing.T) {
	c := testConfig(t)
	c.Socket = filepath.Join(c.StateDir, "control.sock")
	c.TokenFile = filepath.Join(c.StateDir, "token")
	if err := os.WriteFile(c.TokenFile, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(c, &testEngine{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		conn, e := net.Dial("unix", c.Socket)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cl := Client{Socket: c.Socket, TokenFile: c.TokenFile}
	status, _, err := cl.Do(ctx, "GET", "/v1/status", nil)
	if err != nil || status != 200 {
		t.Fatal(status, err)
	}
	other := testConfig(t)
	other.Socket = c.Socket
	other.TokenFile = c.TokenFile
	s2, err := Open(other, &testEngine{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err = s2.Serve(ctx); err == nil {
		t.Fatal("second service stole socket")
	}
	bad := filepath.Join(c.StateDir, "bad-token")
	os.WriteFile(bad, []byte(strings.Repeat("b", 64)), 0600)
	status, _, err = (Client{Socket: c.Socket, TokenFile: bad}).Do(ctx, "POST", "/v1/jobs", map[string]string{"kind": "full"})
	if err != nil || status != 401 {
		t.Fatal("unauthenticated mutation", status, err)
	}
	status, _, err = cl.Do(ctx, "POST", "/v1/jobs", map[string]string{"kind": "restore"})
	if err != nil || status != 400 {
		t.Fatal("arbitrary operation accepted", status, err)
	}
	status, _, err = cl.Do(ctx, "POST", "/v1/jobs", map[string]string{"kind": "full", "actor": "operator"})
	if err != nil || status != 202 {
		t.Fatal(status, err)
	}
}
