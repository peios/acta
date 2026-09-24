package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"acta/internal/codehosts"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func (c *Client) CodeHosts(ctx context.Context) ([]codehosts.Host, error) {
	var out struct {
		Hosts []codehosts.Host `json:"hosts"`
	}
	err := c.Call(ctx, "GET", "code/hosts", nil, &out)
	return out.Hosts, err
}
func (c *Client) CodeHostRequest(ctx context.Context, host, method string, params, output any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.Call(ctx, "POST", "code/hosts/"+host+"/request", codehosts.Request{Method: method, Params: raw}, output)
}
func (c *Client) ConnectCodeHost(ctx context.Context, in codehosts.Heartbeat) (*websocket.Conn, error) {
	server, err := NormalizeURL(c.URL)
	if err != nil {
		return nil, err
	}
	conn, response, err := websocket.Dial(ctx, server+"/api/code/hosts/connect", &websocket.DialOptions{HTTPClient: c.HTTP, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + c.Token}, "User-Agent": []string{"acta-code-host"}}})
	if err != nil {
		if response != nil {
			return nil, &Error{Status: response.StatusCode, Message: fmt.Sprintf("Acta host connection returned HTTP %d", response.StatusCode)}
		}
		return nil, err
	}
	conn.SetReadLimit(codehosts.MaxRequestBytes)
	if err = wsjson.Write(ctx, conn, in); err != nil {
		conn.CloseNow()
		return nil, err
	}
	var ready codehosts.Response
	if err = wsjson.Read(ctx, conn, &ready); err != nil {
		conn.CloseNow()
		return nil, err
	}
	if ready.Error != nil {
		conn.CloseNow()
		return nil, ready.Error
	}
	var ack struct {
		Ready bool `json:"ready"`
	}
	if json.Unmarshal(ready.Result, &ack) != nil || !ack.Ready {
		conn.CloseNow()
		return nil, &codehosts.Problem{Code: "invalid_protocol", Message: "Acta did not accept the host protocol."}
	}
	return conn, nil
}
