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

const (
	maxDiffBytes     = 5 << 20
	githubDiffAccept = "application/vnd.github.v3.diff"
	plainAccept      = "text/plain"
	userAgent        = "CR-Agent"
)

// diffTarget 是一个可尝试的抓取目标：地址 + 期望的 Accept。
type diffTarget struct {
	url    string
	accept string
}

// fetchDiff 抓取 GitHub PR / GitLab MR 的 diff。
//
// GitHub 优先走 REST API（api.github.com/repos/{owner}/{repo}/pulls/{number}）：
// 网页端 <pr>.diff 地址会 302 跳转到 patch-diff.githubusercontent.com，该域名在
// 部分国内机房被解析到不可达 IP，而 API 域名可达且返回完全相同的 diff 内容。
// API 不可用时回退到网页 .diff 地址，保持网络正常环境（如本地开发）的行为不变。
func fetchDiff(ctx context.Context, source string, cfg Config) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(source))
	if err != nil || u.Scheme != "https" {
		return "", "", fmt.Errorf("source 必须是 HTTPS 链接")
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "gitlab.com" {
		return "", "", fmt.Errorf("出于 SSRF 安全限制，仅支持 github.com 和 gitlab.com")
	}

	targets := diffTargets(u, host, cfg)
	if len(targets) == 0 {
		return "", "", fmt.Errorf("无法识别链接，请提供 GitHub PR 或 GitLab MR 地址")
	}

	var lastErr error
	for _, target := range targets {
		diff, err := downloadDiff(ctx, target, cfg)
		if err == nil {
			return target.url, diff, nil
		}
		lastErr = err
	}
	return targets[0].url, "", lastErr
}

func diffTargets(u *url.URL, host string, cfg Config) []diffTarget {
	if host == "gitlab.com" {
		if target, ok := gitlabWebDiff(u); ok {
			return []diffTarget{target}
		}
		return nil
	}
	var targets []diffTarget
	if target, ok := githubAPIDiff(u, cfg); ok {
		targets = append(targets, target)
	}
	if target, ok := githubWebDiff(u); ok {
		targets = append(targets, target)
	}
	return targets
}

// githubAPIDiff 把 PR 链接映射为 REST API 地址：
// https://github.com/{owner}/{repo}/pull/{number}[/files] → {base}/repos/{owner}/{repo}/pulls/{number}
func githubAPIDiff(u *url.URL, cfg Config) (diffTarget, bool) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return diffTarget{}, false
	}
	owner, repo := parts[0], parts[1]
	number := strings.TrimSuffix(strings.TrimSuffix(parts[3], ".diff"), ".patch")
	if owner == "" || repo == "" || number == "" {
		return diffTarget{}, false
	}
	base := strings.TrimSuffix(strings.TrimSpace(cfg.GitHubAPIBase), "/")
	if base == "" {
		base = "https://api.github.com"
	}
	return diffTarget{
		url:    fmt.Sprintf("%s/repos/%s/%s/pulls/%s", base, owner, repo, number),
		accept: githubDiffAccept,
	}, true
}

// githubWebDiff 还原网页端 diff 地址：https://github.com/{owner}/{repo}/pull/{number}.diff
func githubWebDiff(u *url.URL) (diffTarget, bool) {
	if !strings.Contains(u.Path, "/pull/") {
		return diffTarget{}, false
	}
	target := "https://github.com" + strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(target, ".diff") && !strings.HasSuffix(target, ".patch") {
		target += ".diff"
	}
	return diffTarget{url: target, accept: plainAccept}, true
}

// gitlabWebDiff 还原 GitLab MR 的 diff 地址：.../-/merge_requests/{iid}.diff
func gitlabWebDiff(u *url.URL) (diffTarget, bool) {
	if !strings.Contains(u.Path, "/-/merge_requests/") {
		return diffTarget{}, false
	}
	target := "https://gitlab.com" + strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(target, ".diff") && !strings.HasSuffix(target, ".patch") {
		target += ".diff"
	}
	return diffTarget{url: target, accept: plainAccept}, true
}

func downloadDiff(ctx context.Context, target diffTarget, cfg Config) (string, error) {
	return downloadDiffWithClient(ctx, target, cfg, newDiffHTTPClient())
}

func newDiffHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !trustedGitHubDiffHost(req.URL) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func trustedGitHubDiffHost(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.Port() != "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "api.github.com":
		return true
	default:
		return false
	}
}

func downloadDiffWithClient(ctx context.Context, target diffTarget, cfg Config, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.url, nil)
	if err != nil {
		return "", fmt.Errorf("抓取 diff 失败: %w", err)
	}
	req.Header.Set("Accept", target.accept)
	req.Header.Set("User-Agent", userAgent)
	if cfg.GitHubToken != "" && trustedGitHubDiffHost(req.URL) {
		req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("抓取 diff 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		if detail := strings.TrimSpace(redact(string(snippet))); detail != "" {
			return "", fmt.Errorf("抓取 diff 返回 HTTP %d: %s", resp.StatusCode, detail)
		}
		return "", fmt.Errorf("抓取 diff 返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDiffBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取 diff 失败: %w", err)
	}
	if len(body) > maxDiffBytes {
		return "", fmt.Errorf("diff 超过 5 MiB 限制")
	}
	if len(body) == 0 {
		return "", fmt.Errorf("远端 diff 为空")
	}
	return string(body), nil
}
