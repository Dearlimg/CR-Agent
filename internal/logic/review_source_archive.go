package logic

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const (
	reviewSourceArchiveMaxDownloadBytes = 50 << 20
	reviewSourceArchiveMaxExpandedBytes = 100 << 20
	reviewSourceArchiveMaxFileBytes     = 20 << 20
	reviewSourceArchiveMaxFiles         = 20000
	reviewSourceArchiveTimeout          = 90 * time.Second
)

// downloadReviewSourceArchive retrieves and normalizes a ZIP archive from the
// exact head SHA already resolved for the PR source snapshot. GitHub credentials
// are used only for this host-side download and are never included in the ZIP.
func downloadReviewSourceArchive(ctx context.Context, snapshot *reviewSourceSnapshot) ([]byte, error) {
	if snapshot == nil || snapshot.base == nil || !reviewGitHubSHAPattern.MatchString(snapshot.headSHA) {
		return nil, fmt.Errorf("PR 固定 head 快照不可用")
	}
	archiveURL := reviewSourceURL(snapshot.base, "repos", snapshot.owner, snapshot.repo, "zipball", snapshot.headSHA)
	client := &http.Client{
		Timeout: reviewSourceArchiveTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !reviewSourceArchiveRedirectAllowed(snapshot.base, request.URL) {
				return fmt.Errorf("GitHub 源码归档重定向目标无效")
			}
			if !strings.EqualFold(request.URL.Host, snapshot.base.Host) {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 PR 源码归档请求失败")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", userAgent)
	if snapshot.token != "" {
		request.Header.Set("Authorization", "Bearer "+snapshot.token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("下载 PR 固定 head 源码归档失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("下载 PR 源码归档返回 HTTP %d", response.StatusCode)
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, reviewSourceArchiveMaxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 PR 源码归档失败")
	}
	if len(archive) > reviewSourceArchiveMaxDownloadBytes {
		return nil, fmt.Errorf("PR 源码归档超过 50 MiB 限制")
	}
	return normalizeReviewSourceArchive(archive)
}

func reviewSourceArchiveRedirectAllowed(base, target *url.URL) bool {
	if base == nil || target == nil || target.User != nil || target.Fragment != "" {
		return false
	}
	if strings.EqualFold(base.Host, "api.github.com") {
		return target.Scheme == "https" && (strings.EqualFold(target.Hostname(), "codeload.github.com") ||
			strings.EqualFold(target.Hostname(), "github.com"))
	}
	return target.Scheme == base.Scheme && strings.EqualFold(target.Host, base.Host)
}

// normalizeReviewSourceArchive strips GitHub's generated root directory,
// rejects links and unsafe paths, applies decompressed-size limits, and creates
// a fresh ZIP so the sandbox receives only regular repository files.
func normalizeReviewSourceArchive(archive []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("PR 源码归档不是有效 ZIP")
	}
	if len(reader.File) == 0 || len(reader.File) > reviewSourceArchiveMaxFiles {
		return nil, fmt.Errorf("PR 源码归档文件数无效或超过限制")
	}
	root := ""
	seen := make(map[string]struct{}, len(reader.File))
	var expanded int64
	var normalized bytes.Buffer
	writer := zip.NewWriter(&normalized)
	for _, file := range reader.File {
		name := strings.TrimSuffix(file.Name, "/")
		parts := strings.Split(name, "/")
		if len(parts) == 0 || parts[0] == "" {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档包含无效路径")
		}
		if root == "" {
			root = parts[0]
		}
		if parts[0] != root {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档根目录不一致")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 || !file.Mode().IsRegular() {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档包含非普通文件")
		}
		if len(parts) < 2 {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档文件路径缺少根目录")
		}
		relativePath := strings.Join(parts[1:], "/")
		if !reviewSourceValidPath(relativePath) {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档包含不安全路径")
		}
		if _, exists := seen[relativePath]; exists {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档包含重复路径")
		}
		seen[relativePath] = struct{}{}
		if file.UncompressedSize64 > reviewSourceArchiveMaxFileBytes ||
			expanded+int64(file.UncompressedSize64) > reviewSourceArchiveMaxExpandedBytes {
			_ = writer.Close()
			return nil, fmt.Errorf("PR 源码归档解压后超过大小限制")
		}
		content, err := readReviewSourceArchiveFile(file)
		if err != nil {
			_ = writer.Close()
			return nil, err
		}
		expanded += int64(len(content))
		header := &zip.FileHeader{Name: path.Clean(relativePath), Method: zip.Deflate}
		header.SetMode(file.Mode().Perm() & 0o777)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("规范化 PR 源码归档失败")
		}
		if _, err := entry.Write(content); err != nil {
			_ = writer.Close()
			return nil, fmt.Errorf("写入规范化 PR 源码归档失败")
		}
	}
	if len(seen) == 0 {
		_ = writer.Close()
		return nil, fmt.Errorf("PR 源码归档没有普通文件")
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("完成规范化 PR 源码归档失败")
	}
	return normalized.Bytes(), nil
}

func readReviewSourceArchiveFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("读取 PR 源码归档文件失败")
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, reviewSourceArchiveMaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 PR 源码归档文件失败")
	}
	if len(content) > reviewSourceArchiveMaxFileBytes || uint64(len(content)) != file.UncompressedSize64 {
		return nil, fmt.Errorf("PR 源码归档文件大小无效")
	}
	return content, nil
}
