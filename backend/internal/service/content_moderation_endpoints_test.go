package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestNormalizeModerationEndpoint_OnlyAllowsThreeProtocols(t *testing.T) {
	for _, endpoint := range []string{"chat_completions", "responses", "messages"} {
		got, err := NormalizeModerationEndpoint(endpoint)
		require.NoError(t, err)
		require.Equal(t, endpoint, got)
	}
	_, err := NormalizeModerationEndpoint("/v1/anything")
	require.Error(t, err)
}

func TestNormalizeModerationBaseURL_AcceptsOptionalV1Suffix(t *testing.T) {
	require.Equal(t, "https://example.test", normalizeModerationBaseURL("https://example.test/v1/"))
	require.Equal(t, "https://example.test", normalizeModerationBaseURL("https://example.test"))
}

func TestParseModerationProviderConfig_RejectsMissingURLKeyModel(t *testing.T) {
	for name, raw := range map[string][]byte{
		"base_url": []byte(`{"providers":[{"id":"p1","api_key":"secret","model":"safe","endpoint":"chat_completions","enabled":true}]}`),
		"api_key":  []byte(`{"providers":[{"id":"p1","base_url":"https://example.test","model":"safe","endpoint":"chat_completions","enabled":true}]}`),
		"model":    []byte(`{"providers":[{"id":"p1","base_url":"https://example.test","api_key":"secret","endpoint":"chat_completions","enabled":true}]}`),
	} {
		_, err := ParseModerationProviderConfig(raw)
		require.Error(t, err, name)
	}
}

func TestParseStructuredModerationDecision_RejectsAmbiguousOutput(t *testing.T) {
	_, err := ParseStructuredModerationDecision([]byte(`{"reason":"looks dangerous"}`))
	require.Error(t, err)
	_, err = ParseStructuredModerationDecision([]byte(`not json: block`))
	require.Error(t, err)
}

func TestModerationEndpointAdapters(t *testing.T) {
	tests := []struct{ name, endpoint, path, response string }{
		{"Chat", ModerationEndpointChatCompletions, "/v1/chat/completions", `{"choices":[{"message":{"content":"{\"allow\":false,\"categories\":{\"violence\":0.9}}"}}]}`},
		{"Responses", ModerationEndpointResponses, "/v1/responses", `{"output_text":"{\"allow\":true}"}`},
		{"Messages", ModerationEndpointMessages, "/v1/messages", `{"content":[{"text":"{\"allow\":true}"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tt.path, r.URL.Path)
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "test-model", body["model"])
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			decision, err := ModerateWithProvider(t.Context(), server.Client(), ModerationProviderConfig{BaseURL: server.URL, Endpoint: tt.endpoint, Model: "test-model", APIKey: "test-key"}, "input")
			require.NoError(t, err)
			if tt.endpoint == ModerationEndpointChatCompletions {
				require.True(t, decision.Flagged)
			} else {
				require.True(t, decision.Allowed)
			}
		})
	}
}

func TestModerateWithProvider_DoesNotDuplicateV1Suffix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"allow\":true}"}}]}`))
	}))
	defer server.Close()
	decision, err := ModerateWithProvider(t.Context(), server.Client(), ModerationProviderConfig{BaseURL: server.URL + "/v1", Endpoint: ModerationEndpointChatCompletions, Model: "test-model", APIKey: "test-key"}, "input")
	require.NoError(t, err)
	require.True(t, decision.Allowed)
}

func TestCustomModerationProvider_NonTimeoutDisablesAndTimeoutKeepsEnabled(t *testing.T) {
	for _, tt := range []struct {
		name      string
		handler   http.Handler
		timeoutMS int
		disabled  bool
	}{
		{"non-timeout error disables provider", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }), 1000, true},
		{"timeout keeps provider enabled", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusGatewayTimeout)
		}), 20, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			cfg := defaultContentModerationConfig()
			cfg.Providers = []ModerationProviderConfig{{ID: "primary", BaseURL: server.URL, Endpoint: ModerationEndpointChatCompletions, Model: "moderator", APIKey: "test-key", TimeoutMS: tt.timeoutMS, Enabled: true}}
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
			svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil, nil)
			_, err = svc.callModeration(context.Background(), cfg, "input")
			require.Error(t, err)
			var saved ContentModerationConfig
			require.NoError(t, json.Unmarshal([]byte(settings.values[SettingKeyContentModerationConfig]), &saved))
			require.Equal(t, !tt.disabled, saved.Providers[0].Enabled)
		})
	}
}

func TestTestCustomModerationProvider_MapsProviderHTTPFailureAndDisables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	cfg := defaultContentModerationConfig()
	cfg.Providers = []ModerationProviderConfig{{ID: "primary", BaseURL: server.URL, Endpoint: ModerationEndpointChatCompletions, Model: "moderator", APIKey: "test-key", TimeoutMS: 1000, Enabled: true}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: string(raw)}}
	svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil, nil)
	_, err = svc.TestCustomModerationProvider(context.Background(), TestModerationProviderInput{ProviderID: "primary"})
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(settings.values[SettingKeyContentModerationConfig]), &saved))
	require.False(t, saved.Providers[0].Enabled)
}

func TestContentModerationGetConfig_CustomProviderMasksAPIKey(t *testing.T) {
	settings := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settings, nil, nil, nil, nil, nil, nil, nil)
	providers := []ModerationProviderConfig{{ID: "primary", BaseURL: "https://moderator.example", Endpoint: ModerationEndpointChatCompletions, Model: "moderator", APIKey: "provider-canary-secret", Enabled: true}}
	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Providers: &providers})
	require.NoError(t, err)
	require.Len(t, view.Providers, 1)
	require.True(t, view.Providers[0].APIKeyConfigured)
	require.NotContains(t, view.Providers[0].APIKeyMasked, "provider-canary-secret")
}
