// Package daemon — CLI client.
package daemon

import (
	"fmt"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
)

// Client — daemon-a qoşulan CLI client.
type Client struct {
	conn   net.Conn
	client *rpc.Client
}

// Dial — daemon-a qoşulur.
func Dial() (*Client, error) {
	conn, err := net.Dial("unix", Socket)
	if err != nil {
		return nil, fmt.Errorf("daemon-a qoşulma uğursuz (daemon işləyirmi?): %w", err)
	}
	return &Client{
		conn:   conn,
		client: jsonrpc.NewClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

// Run — yeni container işə sal.
func (c *Client) Run(image string, command []string) (*RunReply, error) {
	args := &RunArgs{Image: image, Command: command}
	reply := &RunReply{}
	if err := c.client.Call("API.Run", args, reply); err != nil {
		return nil, err
	}
	return reply, nil
}

// List — bütün container-lər.
func (c *Client) List() ([]interface{}, error) {
	reply := &ListReply{}
	if err := c.client.Call("API.List", &struct{}{}, reply); err != nil {
		return nil, err
	}
	// Container-ləri interface-ə çevir (CLI-də göstərmək üçün).
	out := make([]interface{}, len(reply.Containers))
	for i, c := range reply.Containers {
		out[i] = c
	}
	return out, nil
}

// Stop — container-i dayandır.
func (c *Client) Stop(id string) error {
	return c.client.Call("API.Stop", &IDArgs{ID: id}, &EmptyReply{})
}

// Remove — exited container-i sil.
func (c *Client) Remove(id string) error {
	return c.client.Call("API.Remove", &IDArgs{ID: id}, &EmptyReply{})
}

// Logs — container log-ları.
func (c *Client) Logs(id string) (string, error) {
	reply := &LogsReply{}
	if err := c.client.Call("API.Logs", &IDArgs{ID: id}, reply); err != nil {
		return "", err
	}
	return reply.Content, nil
}

// Stats — container resurs snapshot-u.
func (c *Client) Stats(id string) (*StatsReply, error) {
	reply := &StatsReply{}
	if err := c.client.Call("API.Stats", &IDArgs{ID: id}, reply); err != nil {
		return nil, err
	}
	return reply, nil
}
