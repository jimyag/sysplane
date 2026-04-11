package router_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jimyag/sys-mcp/api/tunnel"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/registry"
	"github.com/jimyag/sys-mcp/internal/sys-mcp-center/router"
)

// fakeStream 实现 pkgstream.TunnelStream 接口用于测试。
type fakeStream struct {
	mu       sync.Mutex
	received []*tunnel.TunnelMessage
	reply    func(msg *tunnel.TunnelMessage) // 收到消息后的回调
}

func (f *fakeStream) Send(msg *tunnel.TunnelMessage) error {
	f.mu.Lock()
	f.received = append(f.received, msg)
	cb := f.reply
	f.mu.Unlock()
	if cb != nil {
		cb(msg)
	}
	return nil
}

func (f *fakeStream) Recv() (*tunnel.TunnelMessage, error) {
	return nil, nil
}

func (f *fakeStream) Context() context.Context {
	return context.Background()
}

func (f *fakeStream) ID() string {
	return "fake-stream"
}

func (f *fakeStream) RemoteAddr() string {
	return "127.0.0.1:0"
}

func TestSendMulti_AllSuccess(t *testing.T) {
	rtr := router.New(5)

	makeRecord := func(hostname string, fs *fakeStream) *registry.AgentRecord {
		return &registry.AgentRecord{
			Hostname:    hostname,
			Status:      registry.StatusOnline,
			RouteStream: fs,
		}
	}

	var records []*registry.AgentRecord
	for i, name := range []string{"host-a", "host-b", "host-c"} {
		idx := i
		hostname := name
		fs := &fakeStream{}
		fs.reply = func(msg *tunnel.TunnelMessage) {
			// 模拟 agent 立刻响应
			req := msg.GetToolRequest()
			if req == nil {
				return
			}
			rtr.Deliver(req.RequestId, &tunnel.TunnelMessage{
				Payload: &tunnel.TunnelMessage_ToolResponse{
					ToolResponse: &tunnel.ToolResponse{
						RequestId:  req.RequestId,
						ResultJson: fmt.Sprintf(`{"host":"%s","idx":%d}`, hostname, idx),
					},
				},
			})
		}
		records = append(records, makeRecord(hostname, fs))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results := rtr.SendMulti(ctx, records, "req-base", "get_hardware_info", "{}")
	if len(results) != 3 {
		t.Fatalf("期望 3 个结果，得到 %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("host %s 出错: %v", r.Hostname, r.Err)
		}
		if r.Result == "" {
			t.Errorf("host %s 结果为空", r.Hostname)
		}
	}
}
