package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	defaultReleaseNotesLocale = "zh-CN"
	releaseNotesCacheFileName = "release-notes-cache.json"
)

type releaseNotesCache struct {
	Entries map[string]releaseNotesCacheEntry `json:"entries"`
}

type releaseNotesCacheEntry struct {
	TranslatedNotes string    `json:"translatedNotes"`
	Summary         string    `json:"summary"`
	Recommendation  string    `json:"recommendation"`
	Locale          string    `json:"locale"`
	Model           string    `json:"model"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type translatedReleasePayload struct {
	TranslatedNotes string `json:"translatedNotes"`
	Summary         string `json:"summary"`
	Recommendation  string `json:"recommendation"`
}

func (m *Manager) LatestReleaseNotes(ctx context.Context, locale, authToken string) (ReleaseInfo, error) {
	release, err := m.CheckUpdate(ctx, authToken)
	if err != nil {
		return ReleaseInfo{}, err
	}
	return m.translateReleaseNotesIfNeeded(ctx, release, locale), nil
}

func (m *Manager) translateReleaseNotesIfNeeded(ctx context.Context, release ReleaseInfo, locale string) ReleaseInfo {
	locale = normalizeReleaseNotesLocale(locale)
	release.ReleaseNotesLocale = locale
	rawNotes := strings.TrimSpace(release.ReleaseNotes)
	if locale == "" || locale == "raw" || rawNotes == "" {
		return release
	}

	cfg, err := m.loadConfig()
	if err != nil {
		return release
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return release
	}

	cacheKey := buildReleaseNotesCacheKey(release, locale)
	cache, _ := m.loadReleaseNotesCache(cfg)
	currentEntry, hasCurrentEntry := cache.Entries[cacheKey]
	if hasCurrentEntry && hasUsableTranslatedNotes(rawNotes, currentEntry.TranslatedNotes, locale) {
		release = applyReleaseNotesCacheEntry(release, rawNotes, currentEntry)
		return release
	}
	legacyCacheKey := buildLegacyReleaseNotesCacheKey(release, locale)
	if entry, ok := cache.Entries[legacyCacheKey]; ok && hasUsableTranslatedNotes(rawNotes, entry.TranslatedNotes, locale) {
		if hasCurrentEntry {
			if strings.TrimSpace(entry.Summary) == "" {
				entry.Summary = strings.TrimSpace(currentEntry.Summary)
			}
			if strings.TrimSpace(entry.Recommendation) == "" {
				entry.Recommendation = strings.TrimSpace(currentEntry.Recommendation)
			}
			if strings.TrimSpace(entry.Model) == "" {
				entry.Model = strings.TrimSpace(currentEntry.Model)
			}
			if strings.TrimSpace(entry.Locale) == "" {
				entry.Locale = strings.TrimSpace(currentEntry.Locale)
			}
		}
		release = applyReleaseNotesCacheEntry(release, rawNotes, entry)
		cache.Entries[cacheKey] = entry
		_ = m.saveReleaseNotesCache(cfg, cache)
		return release
	}

	modelID, err := m.detectTranslationModel(ctx, cfg)
	if err != nil {
		return release
	}
	translated, err := m.translateReleaseNotesViaCPA(ctx, cfg, modelID, locale, release)
	if err != nil {
		return release
	}

	release.OriginalReleaseNotes = rawNotes
	release.ReleaseNotes = translated.TranslatedNotes
	release.UpdateSummary = strings.TrimSpace(translated.Summary)
	if strings.TrimSpace(translated.Recommendation) != "" {
		release.UpdateRecommendation = strings.TrimSpace(translated.Recommendation)
	}
	release.ReleaseNotesLocale = locale
	release.ReleaseNotesModel = modelID

	cache.Entries[cacheKey] = releaseNotesCacheEntry{
		TranslatedNotes: translated.TranslatedNotes,
		Summary:         strings.TrimSpace(translated.Summary),
		Recommendation:  strings.TrimSpace(release.UpdateRecommendation),
		Locale:          locale,
		Model:           modelID,
		UpdatedAt:       time.Now(),
	}
	_ = m.saveReleaseNotesCache(cfg, cache)
	return release
}

func normalizeReleaseNotesLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return defaultReleaseNotesLocale
	}
	return locale
}

func buildReleaseNotesCacheKey(release ReleaseInfo, locale string) string {
	parts := []string{
		strings.TrimSpace(release.CurrentVersion),
		strings.TrimSpace(release.LatestVersion),
		strings.TrimSpace(release.PublishedAt),
		strings.TrimSpace(locale),
	}
	return strings.Join(parts, "|")
}

func buildLegacyReleaseNotesCacheKey(release ReleaseInfo, locale string) string {
	parts := []string{
		strings.TrimSpace(release.CurrentVersion),
		strings.TrimSpace(release.PublishedAt),
		strings.TrimSpace(locale),
	}
	return strings.Join(parts, "|")
}

func applyReleaseNotesCacheEntry(release ReleaseInfo, rawNotes string, entry releaseNotesCacheEntry) ReleaseInfo {
	release.OriginalReleaseNotes = rawNotes
	release.ReleaseNotes = strings.TrimSpace(entry.TranslatedNotes)
	release.UpdateSummary = strings.TrimSpace(entry.Summary)
	if strings.TrimSpace(entry.Recommendation) != "" {
		release.UpdateRecommendation = strings.TrimSpace(entry.Recommendation)
	}
	release.ReleaseNotesLocale = entry.Locale
	release.ReleaseNotesModel = entry.Model
	return release
}

func hasUsableTranslatedNotes(rawNotes, translatedNotes, locale string) bool {
	translatedNotes = strings.TrimSpace(translatedNotes)
	if translatedNotes == "" {
		return false
	}
	return !translationNeedsRetry(rawNotes, translatedNotes, locale)
}

func (m *Manager) loadReleaseNotesCache(cfg DeployConfig) (releaseNotesCache, error) {
	path := filepath.Join(cfg.BaseDir, "ops", releaseNotesCacheFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return releaseNotesCache{Entries: map[string]releaseNotesCacheEntry{}}, nil
		}
		return releaseNotesCache{}, err
	}
	var cache releaseNotesCache
	if err = json.Unmarshal(data, &cache); err != nil {
		return releaseNotesCache{}, err
	}
	if cache.Entries == nil {
		cache.Entries = map[string]releaseNotesCacheEntry{}
	}
	return cache, nil
}

func (m *Manager) saveReleaseNotesCache(cfg DeployConfig, cache releaseNotesCache) error {
	if cache.Entries == nil {
		cache.Entries = map[string]releaseNotesCacheEntry{}
	}
	path := filepath.Join(cfg.BaseDir, "ops", releaseNotesCacheFileName)
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o644)
}

func (m *Manager) detectTranslationModel(ctx context.Context, cfg DeployConfig) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.upstreamBaseURL(cfg)+"/v1/models", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CLIProxyApi-OPS")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("主服务模型列表查询失败: %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	modelIDs := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		modelID := strings.TrimSpace(item.ID)
		if modelID == "" {
			continue
		}
		modelIDs = append(modelIDs, modelID)
	}
	modelID := selectTranslationModel(modelIDs)
	if modelID == "" {
		return "", errors.New("主服务没有可用的文本模型")
	}
	return modelID, nil
}

func selectTranslationModel(modelIDs []string) string {
	preferred := []string{
		"gpt-5-nano",
		"gpt-5-mini",
		"gpt-4.1-mini",
		"gpt-4o-mini",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
		"claude-3-5-haiku",
		"claude-3.5-haiku",
		"qwen",
	}
	for _, keyword := range preferred {
		for _, modelID := range modelIDs {
			if strings.Contains(strings.ToLower(modelID), keyword) {
				return modelID
			}
		}
	}

	blocked := []string{
		"embedding",
		"moderation",
		"tts",
		"whisper",
		"image",
		"audio",
		"transcribe",
		"realtime",
		"search",
		"rerank",
	}
	for _, modelID := range modelIDs {
		lowerID := strings.ToLower(modelID)
		if slices.ContainsFunc(blocked, func(keyword string) bool { return strings.Contains(lowerID, keyword) }) {
			continue
		}
		return modelID
	}
	if len(modelIDs) == 0 {
		return ""
	}
	return modelIDs[0]
}

func (m *Manager) translateReleaseNotesViaCPA(ctx context.Context, cfg DeployConfig, modelID, locale string, release ReleaseInfo) (translatedReleasePayload, error) {
	firstPass, err := m.requestReleaseTranslation(ctx, cfg, modelID, locale, release, false)
	if err != nil {
		return translatedReleasePayload{}, err
	}
	if !translationNeedsRetry(release.ReleaseNotes, firstPass.TranslatedNotes, locale) {
		return firstPass, nil
	}

	retryPass, err := m.requestReleaseTranslation(ctx, cfg, modelID, locale, release, true)
	if err != nil {
		return firstPass, nil
	}
	if translationNeedsRetry(release.ReleaseNotes, retryPass.TranslatedNotes, locale) {
		return firstPass, nil
	}
	return retryPass, nil
}

func (m *Manager) requestReleaseTranslation(ctx context.Context, cfg DeployConfig, modelID, locale string, release ReleaseInfo, strictBodyTranslation bool) (translatedReleasePayload, error) {
	notes := strings.TrimSpace(release.ReleaseNotes)
	systemPrompt := "你是专业的软件版本说明翻译助手。请将输入的 GitHub Release / Changelog 翻译成简体中文。" +
		"保留 Markdown 结构、项目名、版本号、提交哈希、PR 编号、路径、命令、参数、模型名、链接和代码标识原样，不要补充原文没有的信息。" +
		"你还需要基于已给出的建议等级，生成简短中文摘要和更新建议说明。" +
		"输出必须是严格 JSON，字段仅包含 translatedNotes、summary、recommendation。"
	if strictBodyTranslation {
		systemPrompt = "你是严格的软件版本说明翻译助手。请把输入中的英文说明逐条翻译成简体中文。" +
			"只有提交哈希、PR 编号、版本号、模型名、命令、路径、URL 和代码标识允许保留原样，其余英文句子必须翻译。" +
			"如果 translatedNotes 与原文基本相同，视为失败。输出必须是严格 JSON，字段仅包含 translatedNotes、summary、recommendation。"
	}
	payload := map[string]any{
		"model":       modelID,
		"temperature": 0.2,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": systemPrompt,
			},
			{
				"role": "user",
				"content": fmt.Sprintf(
					"当前版本: %s\n最新版本: %s\n落后版本数: %d\n缺失版本: %s\n建议等级: %s\n规则建议: %s\n目标语言: %s\n严格正文翻译: %t\n\n请输出 JSON。\n\n待处理 Release 说明:\n%s",
					blankReleaseValue(release.CurrentVersion),
					blankReleaseValue(release.LatestVersion),
					release.BehindCount,
					strings.Join(release.MissingVersions, ", "),
					blankReleaseValue(release.UpdateRecommendationLevel),
					blankReleaseValue(release.UpdateRecommendation),
					locale,
					strictBodyTranslation,
					notes,
				),
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return translatedReleasePayload{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.upstreamBaseURL(cfg)+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return translatedReleasePayload{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CLIProxyApi-OPS")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return translatedReleasePayload{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return translatedReleasePayload{}, fmt.Errorf("主服务翻译请求失败: %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return translatedReleasePayload{}, err
	}
	if len(result.Choices) == 0 {
		return translatedReleasePayload{}, errors.New("主服务未返回翻译结果")
	}
	translated, err := parseTranslatedReleaseResponse(result.Choices[0].Message.Content)
	if err != nil {
		return translatedReleasePayload{}, err
	}
	if strings.TrimSpace(translated.TranslatedNotes) == "" {
		return translatedReleasePayload{}, errors.New("主服务返回了空翻译结果")
	}
	return translated, nil
}

func translationNeedsRetry(rawNotes, translatedNotes, locale string) bool {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" || locale == "raw" {
		return false
	}
	rawNormalized := normalizeTranslationComparisonText(rawNotes)
	translatedNormalized := normalizeTranslationComparisonText(translatedNotes)
	if rawNormalized == "" || translatedNormalized == "" {
		return false
	}
	if rawNormalized != translatedNormalized {
		return hasMostlyUntranslatedLines(rawNotes, translatedNotes)
	}
	return containsASCIIWords(rawNormalized)
}

func normalizeTranslationComparisonText(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	return strings.Join(strings.Fields(value), " ")
}

func containsASCIIWords(value string) bool {
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			return true
		}
	}
	return false
}

func hasMostlyUntranslatedLines(rawNotes, translatedNotes string) bool {
	rawLines := comparableTranslationLines(rawNotes)
	translatedLines := comparableTranslationLines(translatedNotes)
	if len(rawLines) == 0 || len(translatedLines) == 0 {
		return false
	}

	limit := len(rawLines)
	if len(translatedLines) < limit {
		limit = len(translatedLines)
	}

	comparable := 0
	unchanged := 0
	for index := 0; index < limit; index++ {
		rawLine := rawLines[index]
		if !containsASCIIWords(rawLine) {
			continue
		}
		comparable++
		if normalizeTranslationLine(rawLine) == normalizeTranslationLine(translatedLines[index]) {
			unchanged++
		}
	}
	return comparable >= 2 && unchanged*2 >= comparable
}

func comparableTranslationLines(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func normalizeTranslationLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "*-0123456789. ")
	return strings.Join(strings.Fields(value), " ")
}

func parseTranslatedReleaseResponse(raw string) (translatedReleasePayload, error) {
	content := strings.TrimSpace(raw)
	if content == "" {
		return translatedReleasePayload{}, errors.New("主服务返回了空翻译结果")
	}
	if strings.HasPrefix(content, "```") {
		content = stripMarkdownCodeFence(content)
	}

	var payload translatedReleasePayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return translatedReleasePayload{}, err
	}
	payload.TranslatedNotes = strings.TrimSpace(payload.TranslatedNotes)
	payload.Summary = strings.TrimSpace(payload.Summary)
	payload.Recommendation = strings.TrimSpace(payload.Recommendation)
	return payload, nil
}

func stripMarkdownCodeFence(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```JSON")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func blankReleaseValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未知"
	}
	return value
}
