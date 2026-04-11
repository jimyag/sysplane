// Package router routes tool requests from center to agents and delivers responses.
package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jimyag/sys-mcp/api/tunnel"
	pkgstream "github.com/jimyag/sys-mcp/internal/pkg/stream"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/registry"
)

type pendingSlot struct {
	ch chan *tunnel.TunnelMessage
}

// Router forwards tool requests to agents and correlates responses.
type Router struct {
	timeoutSec int
	pending    sync.Map // requestID -> *pendingSlot
}

// New creates a Router with the given timeout.
func New(timeoutSec int) *Router {
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	return &Router{timeoutSec: timeoutSec}
}

// Send sends a tool request to the agent described by rec and waits for the response.
// Returns the result JSON string or an error.
func (r *Router) Send(ctx context.Context, rec *registry.AgentRecord, requestID, toolName, argsJSON string) (string, error) {
	slot := &pendingSlot{ch: make(chan *tunnel.TunnelMessage, 1)}
	r.pending.Store(requestID, slot)
	defer r.pending.Delete(requestID)

	if err := rec.RouteStream.Send(&tunnel.TunnelMessage{
		Payload: &tunnel.TunnelMessage_ToolRequest{
			ToolRequest: &tunnel.ToolRequest{
				RequestId:  requestID,
				ToolName:   toolName,
				ArgsJson:   argsJSON,
				TargetHost: rec.Hostname,
			},
		},
	}); err != nil {
		return "", fmt.Errorf("router: send to agent %s: %w", rec.Hostname, err)
	}

	timeout := time.Duration(r.timeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("router: timeout waiting for response from %s (request %s)", rec.Hostname, requestID)
	case msg := <-slot.ch:
		switch p := msg.Payload.(type) {
		case *tunnel.TunnelMessage_ToolResponse:
			return p.ToolResponse.ResultJson, nil
		case *tunnel.TunnelMessage_ErrorResponse:
			return "", fmt.Errorf("agent error [%s]: %s", p.ErrorResponse.Code, p.ErrorResponse.Message)
		default:
			return "", fmt.Errorf("router: unexpected response type %T", msg.Payload)
		}
	}
}

// Deliver delivers a response to a waiting Send call.
// Safe to call even if the pending slot has already expired.
func (r *Router) Deliver(requestID string, msg *tunnel.TunnelMessage) {
	val, ok := r.pending.Load(requestID)
	if !ok {
		return
	}
	slot := val.(*pendingSlot)
	select {
	case slot.ch <- msg:
	default:
		// Already delivered or expired.
	}
}

// DeliverStream is a helper that wraps a TunnelStream's messages into Deliver calls.
// It is used by TunnelService to route all incoming responses.
func (r *Router) DeliverFromMessage(msg *tunnel.TunnelMessage) {
	var requestID string
	switch p := msg.Payload.(type) {
	case *tunnel.TunnelMessage_ToolResponse:
		requestID = p.ToolResponse.RequestId
	case *tunnel.TunnelMessage_ErrorResponse:
		requestID = p.ErrorResponse.RequestId
	default:
		return
	}
	r.Deliver(requestID, msg)
}

// DeliverStream is not needed — keep the interface simple.
var _ pkgstream.TunnelStream // import check
