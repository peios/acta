package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func cmdMCPProxy(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("mcp proxy takes at most one profile name")
	}
	profile := "codex"
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		profile = strings.TrimSpace(args[0])
	}

	cfg := loadConfig()
	m, ok := cfg.MCP[profile]
	if !ok || strings.TrimSpace(m.URL) == "" || strings.TrimSpace(m.Token) == "" {
		return fmt.Errorf("no MCP profile %q — run `acta mcp install` first", profile)
	}
	p := &mcpHTTPProxy{
		endpoint: strings.TrimRight(m.URL, "/") + "/mcp",
		token:    m.Token,
		client:   http.DefaultClient,
	}
	return p.run(context.Background())
}

type mcpHTTPProxy struct {
	endpoint string
	token    string
	client   *http.Client
}

func (p *mcpHTTPProxy) run(ctx context.Context) error {
	conn, err := (&mcp.StdioTransport{}).Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	for {
		msg, err := conn.Read(ctx)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		reply, ok, err := p.forward(ctx, msg)
		if err != nil {
			if req, isReq := msg.(*jsonrpc.Request); isReq && req.IsCall() {
				reply = proxyError(req.ID, err)
				ok = true
			} else {
				fmt.Fprintf(os.Stderr, "acta mcp proxy: %v\n", err)
				continue
			}
		}
		if ok {
			if err := conn.Write(ctx, reply); err != nil {
				return err
			}
		}
	}
}

func (p *mcpHTTPProxy) forward(ctx context.Context, msg jsonrpc.Message) (jsonrpc.Message, bool, error) {
	body, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+p.token)

	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return nil, false, fmt.Errorf("Acta MCP HTTP error: %s", msg)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, false, nil
	}
	reply, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		return nil, false, err
	}
	return reply, true, nil
}

func proxyError(id jsonrpc.ID, err error) *jsonrpc.Response {
	return &jsonrpc.Response{
		ID:    id,
		Error: &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: err.Error()},
	}
}
