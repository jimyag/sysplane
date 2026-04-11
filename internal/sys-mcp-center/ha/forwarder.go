package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ForwardRequest 是跨 center 实例转发工具请求的 HTTP 请求体。
type ForwardRequest struct {
	RequestID  string `json:"request_id"`
	ToolName   string `json:"tool_name"`
	ArgsJSON   string `json:"args_json"`
	TargetHost string `json:"target_host"`
}

// ForwardResponse 是转发结果。
type ForwardResponse struct {
	ResultJSON string `json:"result_json,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ForwardToCenter 将工具请求 HTTP POST 到指定 center 实例的 /internal/forward 端点。
func ForwardToCenter(ctx context.Context, internalAddress string, req ForwardRequest) (string, error) {
	body, _ := json.Marshal(req)

	httpCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost,
		"http://"+internalAddress+"/internal/forward",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("ha: build forward request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ha: forward request: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var fwdResp ForwardResponse
	if err := json.Unmarshal(data, &fwdResp); err != nil {
		return "", fmt.Errorf("ha: decode response: %w", err)
	}
	if fwdResp.Error != "" {
		return "", fmt.Errorf("ha: remote error: %s", fwdResp.Error)
	}
	return fwdResp.ResultJSON, nil
}
