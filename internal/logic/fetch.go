package logic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func fetchDiff(ctx context.Context, source string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u.Scheme != "https" {
		return "", "", fmt.Errorf("source 必须是 HTTPS 链接")
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" {
		return "", "", fmt.Errorf("出于 SSRF 安全限制，仅支持 github.com 和 gitlab.com")
	}
	target := strings.TrimSuffix(u.String(), "/")
	if host == "github.com" && strings.Contains(target, "/pull/") {
		if !strings.HasSuffix(target, ".diff") && !strings.HasSuffix(target, ".patch") {
			target += ".diff"
		}
	} else if host == "gitlab.com" && strings.Contains(target, "/-/merge_requests/") {
		if !strings.HasSuffix(target, ".diff") && !strings.HasSuffix(target, ".patch") {
			target += ".diff"
		}
	} else {
		return "", "", fmt.Errorf("无法识别链接，请提供 GitHub PR 或 GitLab MR 地址")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.Header.Set("Accept", "text/plain")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return target, "", fmt.Errorf("抓取 diff 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return target, "", fmt.Errorf("抓取 diff 返回 HTTP %d", resp.StatusCode)
	}
	const maxDiff = 5 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiff+1))
	if err != nil {
		return target, "", fmt.Errorf("读取 diff 失败: %w", err)
	}
	if len(body) > maxDiff {
		return target, "", fmt.Errorf("diff 超过 5 MiB 限制")
	}
	if len(body) == 0 {
		return target, "", fmt.Errorf("远端 diff 为空")
	}
	return target, string(body), nil
}
