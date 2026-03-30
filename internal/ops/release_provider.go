package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type ReleaseProvider interface {
	List(ctx context.Context, currentVersion string) ([]githubRelease, error)
	Latest(ctx context.Context) (githubRelease, error)
}

type githubReleaseProviderFactory struct{}

type githubReleaseProvider struct{}

type githubRelease struct {
	Version     string
	Title       string
	Notes       string
	URL         string
	PublishedAt string
}

func (githubReleaseProviderFactory) Create(options Options) ReleaseProvider {
	return githubReleaseProvider{}
}

func (githubReleaseProvider) List(ctx context.Context, currentVersion string) ([]githubRelease, error) {
	const (
		perPage  = 20
		maxPages = 5
	)

	releases := make([]githubRelease, 0, perPage)
	stopAtCurrent := strings.TrimSpace(currentVersion) != ""

	for page := 1; page <= maxPages; page++ {
		reqURL := fmt.Sprintf("%s?per_page=%d&page=%d", githubReleaseListURL, perPage, page)
		var payload []struct {
			TagName     string `json:"tag_name"`
			Name        string `json:"name"`
			Body        string `json:"body"`
			HTMLURL     string `json:"html_url"`
			PublishedAt string `json:"published_at"`
			Draft       bool   `json:"draft"`
			Prerelease  bool   `json:"prerelease"`
		}
		if err := fetchGitHubJSON(ctx, reqURL, &payload); err != nil {
			return nil, err
		}
		if len(payload) == 0 {
			break
		}

		foundCurrent := false
		for _, item := range payload {
			if item.Draft || item.Prerelease {
				continue
			}
			release := githubRelease{
				Version:     strings.TrimSpace(item.TagName),
				Title:       strings.TrimSpace(item.Name),
				Notes:       strings.TrimSpace(item.Body),
				URL:         strings.TrimSpace(item.HTMLURL),
				PublishedAt: strings.TrimSpace(item.PublishedAt),
			}
			if release.Version == "" {
				continue
			}
			if release.URL == "" {
				release.URL = defaultGitHubReleasePageBase + "/tag/" + release.Version
			}
			releases = append(releases, release)
			if stopAtCurrent && compareVersion(release.Version, currentVersion) <= 0 {
				foundCurrent = true
				break
			}
		}
		if foundCurrent || len(payload) < perPage {
			break
		}
	}

	if len(releases) == 0 {
		latest, err := githubReleaseProvider{}.Latest(ctx)
		if err != nil {
			return nil, err
		}
		return []githubRelease{latest}, nil
	}
	return releases, nil
}

func (githubReleaseProvider) Latest(ctx context.Context) (githubRelease, error) {
	var payload struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
	}
	if err := fetchGitHubJSON(ctx, githubLatestReleaseURL, &payload); err != nil {
		return githubRelease{}, err
	}

	release := githubRelease{
		Version:     strings.TrimSpace(payload.TagName),
		Title:       strings.TrimSpace(payload.Name),
		Notes:       strings.TrimSpace(payload.Body),
		URL:         strings.TrimSpace(payload.HTMLURL),
		PublishedAt: strings.TrimSpace(payload.PublishedAt),
	}
	if release.URL == "" && release.Version != "" {
		release.URL = defaultGitHubReleasePageBase + "/tag/" + release.Version
	}
	return release, nil
}

func fetchGitHubJSON(ctx context.Context, url string, target any) error {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error

	for attempt := 1; attempt <= defaultGitHubRequestRetries; attempt++ {
		if attempt > 1 {
			if err := sleepWithContext(ctx, time.Duration(attempt-1)*250*time.Millisecond); err != nil {
				return err
			}
		}

		body, statusCode, err := doGitHubRequest(ctx, client, url)
		if err != nil {
			lastErr = err
			if !shouldRetryGitHubError(err) || attempt == defaultGitHubRequestRetries {
				return err
			}
			continue
		}
		if statusCode != http.StatusOK {
			lastErr = fmt.Errorf("GitHub 请求失败: %d %s", statusCode, strings.TrimSpace(string(body)))
			if !shouldRetryGitHubStatus(statusCode) || attempt == defaultGitHubRequestRetries {
				if strings.Contains(url, "/releases/latest") {
					return fmt.Errorf("release 查询失败: %d %s", statusCode, strings.TrimSpace(string(body)))
				}
				return fmt.Errorf("release 列表查询失败: %d %s", statusCode, strings.TrimSpace(string(body)))
			}
			continue
		}
		if err := json.Unmarshal(body, target); err != nil {
			lastErr = err
			if !shouldRetryGitHubError(err) || attempt == defaultGitHubRequestRetries {
				return err
			}
			continue
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("GitHub 请求失败")
}

func doGitHubRequest(ctx context.Context, client *http.Client, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CLIProxyApi-OPS")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	return body, resp.StatusCode, nil
}

func shouldRetryGitHubStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func shouldRetryGitHubError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
