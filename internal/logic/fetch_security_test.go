package logic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type diffRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn diffRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestDownloadDiffBindsGitHubTokenToTrustedHost(t *testing.T) {
	const token = "synthetic-github-token"
	cases := []struct {
		name, target, authorization string
	}{
		{"GitLab MR", "https://gitlab.com/group/project/-/merge_requests/42.diff", ""},
		{"GitHub API", "https://api.github.com/repos/owner/repo/pulls/42", "Bearer " + token},
		{"GitHub web", "https://github.com/owner/repo/pull/42.diff", "Bearer " + token},
		{"lookalike host", "https://api.github.com.evil.test/repos/owner/repo/pulls/42", ""},
		{"custom API host", "https://example.test/repos/owner/repo/pulls/42", ""},
		{"plain HTTP", "http://api.github.com/repos/owner/repo/pulls/42", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: diffRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("Authorization"); got != tc.authorization {
					t.Errorf("Authorization = %q, want %q", got, tc.authorization)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("diff")), Header: make(http.Header)}, nil
			})}
			_, err := downloadDiffWithClient(context.Background(), diffTarget{url: tc.target, accept: plainAccept}, Config{GitHubToken: token}, client)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDownloadDiffDropsGitHubTokenOnCrossPlatformRedirect(t *testing.T) {
	client := newDiffHTTPClient()
	requests := 0
	client.Transport = diffRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if got := req.Header.Get("Authorization"); got != "Bearer synthetic-github-token" {
				t.Errorf("initial GitHub Authorization = %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"https://gitlab.com/group/project/-/merge_requests/42.diff"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}
		if req.URL.Hostname() != "gitlab.com" || req.Header.Get("Authorization") != "" {
			t.Errorf("redirect leaked GitHub Authorization to %s: %q", req.URL, req.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("diff"))}, nil
	})
	_, err := downloadDiffWithClient(context.Background(), diffTarget{url: "https://github.com/owner/repo/pull/42.diff"}, Config{GitHubToken: "synthetic-github-token"}, client)
	if err != nil || requests != 2 {
		t.Fatalf("redirect requests=%d err=%v", requests, err)
	}
}
