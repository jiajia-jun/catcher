// Package ai 实现用户主动开启的云端 JSON 生成接口，与平台抓取凭据完全隔离。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"catcher/internal/domain/entity"
)

type Client struct{ Transport http.RoundTripper }

func Endpoint(base string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("模型地址必须是无凭据、查询参数和片段的 HTTPS 基地址")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	return u.String(), nil
}

func (c *Client) Generate(ctx context.Context, in entity.AIModelRequest) (entity.AIModelResult, error) {
	out := entity.AIModelResult{}
	endpoint, err := Endpoint(in.Settings.BaseURL)
	if err != nil {
		return out, err
	}
	payload := map[string]any{"model": in.Settings.Model, "messages": []map[string]string{{"role": "system", "content": in.System}, {"role": "user", "content": in.User}}, "stream": false, "max_tokens": in.Settings.MaxOutputTokens, "response_format": map[string]string{"type": "json_object"}}
	// DeepSeek 的 JSON 模式不需要推理内容；显式关闭以控制延迟和预算。
	if u, _ := url.Parse(endpoint); strings.EqualFold(u.Hostname(), "api.deepseek.com") {
		payload["thinking"] = map[string]string{"type": "disabled"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return out, fmt.Errorf("构造模型请求失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("模型请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.Settings.Key)
	client := http.Client{Transport: c.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("模型请求已取消或超时")
		}
		return out, fmt.Errorf("模型网络请求失败，请检查服务地址和网络")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("模型服务返回 HTTP %d，请检查 Key、模型或服务额度", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return out, fmt.Errorf("模型响应读取失败或超过大小限制")
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			Input  int `json:"prompt_tokens"`
			Output int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return out, fmt.Errorf("模型服务响应格式无效")
	}
	if envelope.Usage != nil && envelope.Usage.Input >= 0 && envelope.Usage.Output >= 0 && envelope.Usage.Input+envelope.Usage.Output > 0 {
		out.InputTokens = envelope.Usage.Input
		out.OutputTokens = envelope.Usage.Output
		out.UsageKnown = true
	}
	if len(envelope.Choices) != 1 || envelope.Choices[0].FinishReason != "stop" {
		return out, fmt.Errorf("模型输出未完整结束，请减少所选作业或调整输出上限")
	}
	out.Content = strings.TrimSpace(envelope.Choices[0].Message.Content)
	if out.Content == "" || !json.Valid([]byte(out.Content)) {
		return out, fmt.Errorf("模型未返回有效 JSON，请更换支持 JSON 输出的模型")
	}
	return out, nil
}
