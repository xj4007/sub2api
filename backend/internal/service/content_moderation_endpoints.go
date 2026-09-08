package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ModerationEndpointChatCompletions = "chat_completions"
	ModerationEndpointResponses       = "responses"
	ModerationEndpointMessages        = "messages"
)

// ModerationProviderConfig describes an independently scheduled custom review provider.
// APIKey is write-only: config reads must return ContentModerationProviderView instead.
type ModerationProviderConfig struct {
	ID        string `json:"id"`
	BaseURL   string `json:"base_url"`
	Endpoint  string `json:"endpoint"`
	Model     string `json:"model"`
	APIKey    string `json:"api_key"`
	Priority  int    `json:"priority,omitempty"`
	Enabled   bool   `json:"enabled"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
	Note      string `json:"note,omitempty"`
}

type ContentModerationProviderView struct {
	ID               string `json:"id"`
	BaseURL          string `json:"base_url"`
	Endpoint         string `json:"endpoint"`
	Model            string `json:"model"`
	Priority         int    `json:"priority"`
	Enabled          bool   `json:"enabled"`
	TimeoutMS        int    `json:"timeout_ms"`
	Note             string `json:"note,omitempty"`
	APIKeyConfigured bool   `json:"api_key_configured"`
	APIKeyMasked     string `json:"api_key_masked"`
}

type ModerationDecision struct {
	Allowed    bool               `json:"allow"`
	Flagged    bool               `json:"flagged"`
	Categories map[string]float64 `json:"categories,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Confidence float64            `json:"confidence,omitempty"`
	ProviderID string             `json:"provider_id,omitempty"`
}

type TestModerationProviderInput struct {
	ProviderID string
	APIKey     string
	Prompt     string
}

type TestModerationProviderResult struct {
	ProviderID string             `json:"provider_id"`
	Allowed    bool               `json:"allow"`
	Flagged    bool               `json:"flagged"`
	Reason     string             `json:"reason,omitempty"`
	Categories map[string]float64 `json:"categories,omitempty"`
}

type moderationProviderError struct {
	ProviderID string
	Err        error
}

func (e *moderationProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "moderation provider error"
	}
	return e.Err.Error()
}

func (e *moderationProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NormalizeModerationEndpoint(endpoint string) (string, error) {
	switch strings.TrimSpace(endpoint) {
	case ModerationEndpointChatCompletions, ModerationEndpointResponses, ModerationEndpointMessages:
		return strings.TrimSpace(endpoint), nil
	default:
		return "", fmt.Errorf("unsupported moderation endpoint %q", endpoint)
	}
}

func normalizeModerationProviders(providers []ModerationProviderConfig) ([]ModerationProviderConfig, error) {
	if len(providers) == 0 {
		return []ModerationProviderConfig{}, nil
	}
	result := make([]ModerationProviderConfig, len(providers))
	copy(result, providers)
	seen := make(map[string]struct{}, len(result))
	for i := range result {
		provider := &result[i]
		provider.ID = strings.TrimSpace(provider.ID)
		if provider.ID == "" {
			return nil, fmt.Errorf("provider id is required")
		}
		if _, exists := seen[provider.ID]; exists {
			return nil, fmt.Errorf("duplicate moderation provider id %q", provider.ID)
		}
		seen[provider.ID] = struct{}{}
		provider.BaseURL = normalizeModerationBaseURL(provider.BaseURL)
		provider.Model = strings.TrimSpace(provider.Model)
		provider.APIKey = strings.TrimSpace(provider.APIKey)
		provider.Note = strings.TrimSpace(provider.Note)
		endpoint, err := NormalizeModerationEndpoint(provider.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", provider.ID, err)
		}
		provider.Endpoint = endpoint
		parsed, err := url.Parse(provider.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("provider %q: invalid base_url", provider.ID)
		}
		if provider.Enabled && (provider.Model == "" || provider.APIKey == "") {
			return nil, fmt.Errorf("provider %q: model and api_key are required when enabled", provider.ID)
		}
		if provider.TimeoutMS < 0 || provider.TimeoutMS > maxContentModerationTimeoutMS {
			return nil, fmt.Errorf("provider %q: timeout_ms is out of range", provider.ID)
		}
	}
	return result, nil
}

func normalizeModerationBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if strings.HasSuffix(strings.ToLower(base), "/v1") {
		base = strings.TrimRight(base[:len(base)-len("/v1")], "/")
	}
	return base
}

func ParseModerationProviderConfig(raw []byte) ([]ModerationProviderConfig, error) {
	var document struct {
		Providers []ModerationProviderConfig `json:"providers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("parse moderation provider config: %w", err)
	}
	return normalizeModerationProviders(document.Providers)
}

func ParseStructuredModerationDecision(raw []byte) (ModerationDecision, error) {
	var result ModerationDecision
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return result, fmt.Errorf("moderation decision is not json: %w", err)
	}
	allow, ok := fields["allow"]
	if !ok {
		return result, errors.New("moderation decision missing allow")
	}
	if err := json.Unmarshal(allow, &result.Allowed); err != nil {
		return result, errors.New("moderation decision allow must be boolean")
	}
	if value, ok := fields["flagged"]; ok {
		_ = json.Unmarshal(value, &result.Flagged)
	}
	if !result.Allowed {
		result.Flagged = true
	}
	if value, ok := fields["categories"]; ok {
		_ = json.Unmarshal(value, &result.Categories)
	}
	if value, ok := fields["reason"]; ok {
		_ = json.Unmarshal(value, &result.Reason)
	}
	if value, ok := fields["confidence"]; ok {
		_ = json.Unmarshal(value, &result.Confidence)
	}
	return result, nil
}

func (s *ContentModerationService) callCustomModerationProviders(ctx context.Context, cfg *ContentModerationConfig, input any) (*moderationAPIResult, error) {
	enabled := make([]ModerationProviderConfig, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if provider.Enabled {
			enabled = append(enabled, provider)
		}
	}
	if len(enabled) == 0 {
		return nil, errors.New("no enabled moderation provider available")
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		return enabled[i].Priority < enabled[j].Priority
	})
	start := int(s.customProviderCursor.Add(1)-1) % len(enabled)
	var lastErr error
	for offset := 0; offset < len(enabled); offset++ {
		provider := enabled[(start+offset)%len(enabled)]
		timeoutMS := provider.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = cfg.TimeoutMS
		}
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		client, err := s.moderationHTTPClient(reqCtx, cfg)
		if err == nil {
			text, marshalErr := json.Marshal(input)
			if value, ok := input.(string); ok {
				text = []byte(value)
			} else if marshalErr != nil {
				err = marshalErr
			}
			if err == nil {
				decision, callErr := ModerateWithProvider(reqCtx, client, provider, string(text))
				err = callErr
				if err == nil {
					cancel()
					return &moderationAPIResult{Flagged: decision.Flagged || !decision.Allowed, CategoryScores: decision.Categories, ProviderID: provider.ID}, nil
				}
			}
		}
		cancel()
		if err != nil && !isModerationProviderTimeout(err) {
			s.disableModerationProvider(ctx, cfg, provider.ID)
		}
		if err != nil {
			lastErr = &moderationProviderError{ProviderID: provider.ID, Err: err}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no enabled moderation provider available")
	}
	return nil, lastErr
}

func (s *ContentModerationService) TestCustomModerationProvider(ctx context.Context, input TestModerationProviderInput) (*TestModerationProviderResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	for _, provider := range cfg.Providers {
		if provider.ID != strings.TrimSpace(input.ProviderID) {
			continue
		}
		if strings.TrimSpace(input.APIKey) != "" {
			provider.APIKey = strings.TrimSpace(input.APIKey)
		}
		if provider.APIKey == "" {
			return nil, infraerrors.BadRequest("CONTENT_MODERATION_PROVIDER_API_KEY_MISSING", "Provider 未配置 API Key")
		}
		prompt := strings.TrimSpace(input.Prompt)
		if prompt == "" {
			prompt = "This is a provider connectivity test. Return allow=true as JSON."
		}
		timeoutMS := provider.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = cfg.TimeoutMS
		}
		testCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		client, callErr := s.moderationHTTPClient(testCtx, cfg)
		if callErr == nil {
			var decision ModerationDecision
			decision, callErr = ModerateWithProvider(testCtx, client, provider, prompt)
			if callErr == nil {
				cancel()
				return &TestModerationProviderResult{ProviderID: provider.ID, Allowed: decision.Allowed, Flagged: decision.Flagged, Reason: decision.Reason, Categories: decision.Categories}, nil
			}
		}
		cancel()
		if !isModerationProviderTimeout(callErr) {
			s.disableModerationProvider(ctx, cfg, provider.ID)
		}
		if isModerationProviderTimeout(callErr) {
			return nil, infraerrors.GatewayTimeout("CONTENT_MODERATION_PROVIDER_TIMEOUT", "Provider 请求超时")
		}
		return nil, infraerrors.New(http.StatusBadGateway, "CONTENT_MODERATION_PROVIDER_REQUEST_FAILED", "Provider 请求失败，请检查 URL、协议、模型和 API Key")
	}
	return nil, infraerrors.NotFound("CONTENT_MODERATION_PROVIDER_NOT_FOUND", fmt.Sprintf("Provider %q 不存在", input.ProviderID))
}

// disableModerationProvider persists a non-timeout provider failure. Timeout
// errors deliberately leave the provider enabled: slow remote review is not a
// configuration failure and the main request remains fail-open either way.
func (s *ContentModerationService) disableModerationProvider(ctx context.Context, cfg *ContentModerationConfig, providerID string) {
	if s == nil || s.settingRepo == nil || cfg == nil || strings.TrimSpace(providerID) == "" {
		return
	}
	next := cloneContentModerationConfig(cfg)
	for index := range next.Providers {
		if next.Providers[index].ID == providerID {
			next.Providers[index].Enabled = false
			break
		}
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return
	}
	if err := s.saveModerationConfig(ctx, string(raw)); err != nil {
		return
	}
	s.replaceRuntimeConfig(next, raw)
}

func isModerationProviderTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func ModerateWithProvider(ctx context.Context, client *http.Client, provider ModerationProviderConfig, input string) (ModerationDecision, error) {
	if client == nil {
		client = http.DefaultClient
	}
	path := map[string]string{
		ModerationEndpointChatCompletions: "/v1/chat/completions",
		ModerationEndpointResponses:       "/v1/responses",
		ModerationEndpointMessages:        "/v1/messages",
	}[provider.Endpoint]
	prompt := "Review the following content. Return only JSON: {\"allow\":true,\"categories\":{},\"reason\":\"\",\"confidence\":0}. Content: " + input
	var payload any
	switch provider.Endpoint {
	case ModerationEndpointResponses:
		payload = map[string]any{"model": provider.Model, "input": prompt}
	case ModerationEndpointMessages:
		payload = map[string]any{"model": provider.Model, "max_tokens": 256, "messages": []any{map[string]string{"role": "user", "content": prompt}}}
	default:
		payload = map[string]any{"model": provider.Model, "messages": []any{map[string]string{"role": "user", "content": prompt}}}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ModerationDecision{}, err
	}
	baseURL := normalizeModerationBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return ModerationDecision{}, err
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ModerationDecision{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ModerationDecision{}, fmt.Errorf("moderation provider status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return ModerationDecision{}, errors.New("moderation provider returned empty response")
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ModerationDecision{}, err
	}
	text := envelope.OutputText
	if text == "" && len(envelope.Choices) > 0 {
		text = envelope.Choices[0].Message.Content
	}
	if text == "" && len(envelope.Output) > 0 && len(envelope.Output[0].Content) > 0 {
		text = envelope.Output[0].Content[0].Text
	}
	if text == "" && len(envelope.Content) > 0 {
		text = envelope.Content[0].Text
	}
	if text == "" {
		return ModerationDecision{}, errors.New("moderation provider response missing decision")
	}
	return ParseStructuredModerationDecision([]byte(text))
}
