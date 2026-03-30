package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSelectTranslationModel(t *testing.T) {
	t.Parallel()

	modelID := selectTranslationModel([]string{
		"text-embedding-3-large",
		"gemini-2.5-flash",
		"gpt-4o",
	})
	if modelID != "gemini-2.5-flash" {
		t.Fatalf("selectTranslationModel returned %q", modelID)
	}
}

func TestTranslateReleaseNotesIfNeededUsesCPAMainServiceAndCache(t *testing.T) {
	t.Parallel()

	var modelsCalls atomic.Int32
	var translateCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			modelsCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "text-embedding-3-large"},
					{"id": "gemini-2.5-flash"},
				},
			})
		case "/v1/chat/completions":
			translateCalls.Add(1)
			var payload struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request failed: %v", err)
			}
			if payload.Model != "gemini-2.5-flash" {
				t.Fatalf("unexpected model %q", payload.Model)
			}
			if len(payload.Messages) == 0 || !strings.Contains(payload.Messages[len(payload.Messages)-1].Content, "fix: keep config") {
				t.Fatalf("unexpected translate payload: %+v", payload.Messages)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": `{"translatedNotes":"## 更新说明\n- 修复：保留配置持久化","summary":"本次更新重点修复配置持久化问题。","recommendation":"建议尽快更新，避免配置修改丢失。"}`,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
		Overrides: OverrideConfig{
			APIKey: "test-api-key",
		},
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	raw := ReleaseInfo{
		LatestVersion: "v1.2.3",
		PublishedAt:   "2026-03-30T00:00:00Z",
		ReleaseNotes:  "fix: keep config",
	}

	got := manager.translateReleaseNotesIfNeeded(context.Background(), raw, "zh-CN")
	if got.ReleaseNotes != "## 更新说明\n- 修复：保留配置持久化" {
		t.Fatalf("translated notes mismatch: %q", got.ReleaseNotes)
	}
	if got.OriginalReleaseNotes != raw.ReleaseNotes {
		t.Fatalf("original notes mismatch: %q", got.OriginalReleaseNotes)
	}
	if got.ReleaseNotesModel != "gemini-2.5-flash" {
		t.Fatalf("translation model mismatch: %q", got.ReleaseNotesModel)
	}
	if got.UpdateSummary != "本次更新重点修复配置持久化问题。" {
		t.Fatalf("translation summary mismatch: %q", got.UpdateSummary)
	}
	if got.UpdateRecommendation != "建议尽快更新，避免配置修改丢失。" {
		t.Fatalf("translation recommendation mismatch: %q", got.UpdateRecommendation)
	}

	cachePath := filepath.Join(baseDir, "ops", releaseNotesCacheFileName)
	if _, err = os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	gotCached := manager.translateReleaseNotesIfNeeded(context.Background(), raw, "zh-CN")
	if gotCached.ReleaseNotes != got.ReleaseNotes {
		t.Fatalf("cached translation mismatch: %q", gotCached.ReleaseNotes)
	}
	if modelsCalls.Load() != 1 || translateCalls.Load() != 1 {
		t.Fatalf("expected cache hit, models=%d translate=%d", modelsCalls.Load(), translateCalls.Load())
	}
}

func TestTranslateReleaseNotesIfNeededRetriesWhenBodyRemainsUnchanged(t *testing.T) {
	t.Parallel()

	var translateCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-5-mini"},
				},
			})
		case "/v1/chat/completions":
			call := translateCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			content := `{"translatedNotes":"fix: keep config","summary":"第一次调用仍是原文。","recommendation":"建议重试翻译。"}`
			if call == 2 {
				content = `{"translatedNotes":"修复：保留配置","summary":"已翻译完成。","recommendation":"可以按常规窗口更新。"}`
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": content,
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{
		BaseDir:         baseDir,
		WorkspaceRoot:   baseDir,
		UpstreamBaseURL: server.URL,
		Overrides: OverrideConfig{
			APIKey: "test-api-key",
		},
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	got := manager.translateReleaseNotesIfNeeded(context.Background(), ReleaseInfo{
		CurrentVersion: "v1.0.0",
		LatestVersion:  "v1.2.3",
		PublishedAt:    "2026-03-30T00:00:00Z",
		ReleaseNotes:   "fix: keep config",
	}, "zh-CN")

	if got.ReleaseNotes != "修复：保留配置" {
		t.Fatalf("translated notes mismatch: %q", got.ReleaseNotes)
	}
	if translateCalls.Load() != 2 {
		t.Fatalf("expected second translation attempt, got %d", translateCalls.Load())
	}
}

func TestTranslateReleaseNotesIfNeededUsesLegacyCacheKey(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	manager, err := NewManager(Options{
		BaseDir:       baseDir,
		WorkspaceRoot: baseDir,
		Overrides: OverrideConfig{
			APIKey: "test-api-key",
		},
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg, err := manager.loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	cache := releaseNotesCache{
		Entries: map[string]releaseNotesCacheEntry{
			"v6.9.6|2026-03-29T14:26:17Z|zh-CN": {
				TranslatedNotes: "## 更新日志\n- 已迁移旧缓存",
				Summary:         "摘要",
				Recommendation:  "建议",
				Locale:          "zh-CN",
				Model:           "gpt-5-codex",
			},
			"v6.9.6|v6.9.6|2026-03-29T14:26:17Z|zh-CN": {
				TranslatedNotes: "## 更新日志\n* Merge pull request #1 from repo/branch\n* fix(config): keep config",
				Summary:         "现有摘要",
				Recommendation:  "现有建议",
				Locale:          "zh-CN",
				Model:           "gpt-5-mini",
			},
		},
	}
	if err = manager.saveReleaseNotesCache(cfg, cache); err != nil {
		t.Fatalf("saveReleaseNotesCache failed: %v", err)
	}

	got := manager.translateReleaseNotesIfNeeded(context.Background(), ReleaseInfo{
		CurrentVersion: "v6.9.6",
		LatestVersion:  "v6.9.6",
		PublishedAt:    "2026-03-29T14:26:17Z",
		ReleaseNotes:   "## Changelog\n* Merge pull request #1 from repo/branch\n* fix(config): keep config",
	}, "zh-CN")

	if got.ReleaseNotes != "## 更新日志\n- 已迁移旧缓存" {
		t.Fatalf("legacy cache translation mismatch: %q", got.ReleaseNotes)
	}
	if got.UpdateSummary != "摘要" {
		t.Fatalf("legacy cache summary mismatch: %q", got.UpdateSummary)
	}
	if got.UpdateRecommendation != "建议" {
		t.Fatalf("legacy cache recommendation mismatch: %q", got.UpdateRecommendation)
	}

	loadedCache, err := manager.loadReleaseNotesCache(cfg)
	if err != nil {
		t.Fatalf("loadReleaseNotesCache failed: %v", err)
	}
	entry, ok := loadedCache.Entries["v6.9.6|v6.9.6|2026-03-29T14:26:17Z|zh-CN"]
	if !ok {
		t.Fatal("expected migrated cache key to be written")
	}
	if entry.Summary != "摘要" || entry.Recommendation != "建议" {
		t.Fatalf("expected migrated entry to preserve usable summary and recommendation: %#v", entry)
	}
}

func TestTranslationNeedsRetryDetectsMostlyUntranslatedBody(t *testing.T) {
	t.Parallel()

	raw := "## 更新日志\n* Merge pull request #1 from repo/branch\n* fix(config): keep config"
	translated := "## 更新日志\n* Merge pull request #1 from repo/branch\n* fix(config): keep config"
	if !translationNeedsRetry(raw, translated, "zh-CN") {
		t.Fatal("expected untranslated English body to require retry")
	}

	betterTranslated := "## 更新日志\n* 合并拉取请求 #1，来自 repo/branch\n* fix(config): 保留配置"
	if translationNeedsRetry(raw, betterTranslated, "zh-CN") {
		t.Fatal("expected translated body to pass")
	}
}
