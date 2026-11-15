//go:build unit

package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func storedCustomEmail() map[string]string {
	return map[string]string{
		service.SettingKeyEmailProvider:             "custom",
		service.SettingKeyCustomEmailBaseURL:        "http://10.0.0.5:8081",
		service.SettingKeyCustomEmailToken:          "ezm_old",
		service.SettingKeyCustomEmailFrom:           "billing@example.com",
		service.SettingKeyCustomEmailTimeoutSeconds: "60",
	}
}

func TestUpdateSettingsCustomEmailWritesNormalizedValues(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{
		"email_provider": " Custom ", "custom_email_base_url": " http://10.0.0.5:8081/ ",
		"custom_email_token": " ezm_abc\n", // Review Focus #1
		"custom_email_from":  "Billing <billing@example.com>", "custom_email_timeout_seconds": 500,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "custom", repo.values[service.SettingKeyEmailProvider])
	require.Equal(t, "http://10.0.0.5:8081", repo.values[service.SettingKeyCustomEmailBaseURL])
	require.Equal(t, "ezm_abc", repo.values[service.SettingKeyCustomEmailToken])
	require.Equal(t, "billing@example.com", repo.values[service.SettingKeyCustomEmailFrom])
	require.Equal(t, "300", repo.values[service.SettingKeyCustomEmailTimeoutSeconds])
	require.Contains(t, rec.Body.String(), `"custom_email_token_configured":true`)
	require.NotContains(t, rec.Body.String(), "ezm_abc")
}

func TestUpdateSettingsCustomEmailKeepsStoredTokenWhenBlank(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, storedCustomEmail())
	rec := doUpdateSettings(t, h, map[string]any{"custom_email_token": "   "}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ezm_old", repo.values[service.SettingKeyCustomEmailToken])
}

func TestUpdateSettingsPartialPayloadKeepsCustomEmailSettings(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, storedCustomEmail())
	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	for k, v := range storedCustomEmail() {
		require.Equal(t, v, repo.values[k], k)
	}
}

func TestUpdateSettingsSwitchBackToSMTPKeepsCustomEmailSettings(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, storedCustomEmail())
	rec := doUpdateSettings(t, h, map[string]any{"email_provider": "smtp"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "smtp", repo.values[service.SettingKeyEmailProvider])
	for k, v := range storedCustomEmail() {
		if k == service.SettingKeyEmailProvider {
			continue
		}
		require.Equal(t, v, repo.values[k], k)
	}

	// 再切回 custom，不用重填令牌
	rec = doUpdateSettings(t, h, map[string]any{"email_provider": "custom"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "custom", repo.values[service.SettingKeyEmailProvider])
	require.Equal(t, "ezm_old", repo.values[service.SettingKeyCustomEmailToken])
}

func TestUpdateSettingsCustomEmailRequiresBaseURLAndToken(t *testing.T) {
	cases := map[string]map[string]any{
		"missing token":    {"email_provider": "custom", "custom_email_base_url": "http://10.0.0.5:8081"},
		"missing base url": {"email_provider": "custom", "custom_email_token": "ezm_x"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := doUpdateSettings(t, h, body, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "required when email_provider is custom")
			_, ok := repo.values[service.SettingKeyEmailProvider]
			require.False(t, ok)
		})
	}
}

func TestUpdateSettingsCustomEmailAcceptsStoredToken(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyCustomEmailToken: "ezm_old"})
	rec := doUpdateSettings(t, h, map[string]any{
		"email_provider": "custom", "custom_email_base_url": "http://10.0.0.5:8081",
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestUpdateSettingsRejectsInvalidEmailProvider(t *testing.T) {
	for _, v := range []any{"", "sendgrid", nil} {
		t.Run(fmt.Sprintf("%v", v), func(t *testing.T) {
			h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := doUpdateSettings(t, h, map[string]any{"email_provider": v}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "email_provider must be one of")
		})
	}
}

func TestUpdateSettingsRejectsInvalidCustomEmailBaseURL(t *testing.T) {
	for _, u := range []string{"ftp://10.0.0.5", "http://h?x=1", "http://h?", "http://h#a", "http://u:p@h"} {
		t.Run(u, func(t *testing.T) {
			h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := doUpdateSettings(t, h, map[string]any{"custom_email_base_url": u}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "invalid custom_email_base_url")
		})
	}
}

func TestUpdateSettingsRejectsInvalidCustomEmailFrom(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{"custom_email_from": "not-an-email"}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid custom_email_from")
}

func TestUpdateSettingsCustomEmailTimeoutNormalization(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{{0, "45"}, {5, "10"}, {60, "60"}, {301, "300"}}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("in=%d", tc.in), func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := doUpdateSettings(t, h, map[string]any{"custom_email_timeout_seconds": tc.in}, nil)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tc.want, repo.values[service.SettingKeyCustomEmailTimeoutSeconds])
		})
	}
}

func TestDiffSettingsCustomEmailChanges(t *testing.T) {
	before := &service.SystemSettings{EmailProvider: "smtp", CustomEmailTimeoutSeconds: 45}
	after := &service.SystemSettings{
		EmailProvider: "custom", CustomEmailBaseURL: "http://10.0.0.5:8081",
		CustomEmailFrom: "billing@example.com", CustomEmailTimeoutSeconds: 60,
	}
	names := []string{"email_provider", "custom_email_base_url", "custom_email_token", "custom_email_from", "custom_email_timeout_seconds"}

	changed := diffSettings(before, after, nil, nil, UpdateSettingsRequest{CustomEmailToken: "ezm"})
	for _, n := range names {
		require.Contains(t, changed, n)
	}
	// 前后无变化且令牌为空：5 个字段名都不应出现
	changed = diffSettings(after, after, nil, nil, UpdateSettingsRequest{})
	for _, n := range names {
		require.NotContains(t, changed, n)
	}
}

func TestResolveCustomEmailSettingsUpdateKeepsUnsentFields(t *testing.T) {
	previous := &service.SystemSettings{EmailProvider: "custom", CustomEmailBaseURL: "http://10.0.0.5:8081",
		CustomEmailTokenConfigured: true, CustomEmailFrom: "billing@example.com", CustomEmailTimeoutSeconds: 60}
	out, err := resolveCustomEmailSettingsUpdate(UpdateSettingsRequest{}, previous, map[string]json.RawMessage{})
	require.NoError(t, err)
	require.Equal(t, customEmailSettingsUpdate{Provider: "custom", BaseURL: "http://10.0.0.5:8081", From: "billing@example.com", TimeoutSeconds: 60}, out)
	after := &service.SystemSettings{EmailProvider: out.Provider, CustomEmailBaseURL: out.BaseURL, CustomEmailFrom: out.From, CustomEmailTimeoutSeconds: out.TimeoutSeconds}
	changed := diffSettings(previous, after, nil, nil, settingsAuditRequest(UpdateSettingsRequest{CustomEmailToken: " \n"}))
	for _, name := range []string{"email_provider", "custom_email_base_url", "custom_email_token", "custom_email_from", "custom_email_timeout_seconds"} {
		require.NotContains(t, changed, name)
	}
}

func TestUpdateSettingsCustomEmailValidatesEffectiveProvider(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, storedCustomEmail())
	rec := doUpdateSettings(t, h, map[string]any{"custom_email_base_url": ""}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "required when email_provider is custom")
	require.Equal(t, "http://10.0.0.5:8081", repo.values[service.SettingKeyCustomEmailBaseURL])
}

func newCustomEmailTestHandler(t *testing.T, stored map[string]string) *SettingHandler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: stored}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, service.NewEmailService(repo, nil), nil, nil, nil, nil, nil)
}

func postSettingsJSON(t *testing.T, handle gin.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handle(c)
	return rec
}

type fakeEzEmail struct {
	*httptest.Server
	mu     sync.Mutex
	auths  []string
	bodies []string
}

func newFakeEzEmail(t *testing.T, status int, respBody string) *fakeEzEmail {
	t.Helper()
	f := &fakeEzEmail{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.bodies = append(f.bodies, string(b))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeEzEmail) requests() (auths, bodies []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...), append([]string(nil), f.bodies...)
}

func TestTestSMTPConnectionCustomProviderSuccess(t *testing.T) {
	ez := newFakeEzEmail(t, 400, `{"code":"invalid_request"}`)
	h := newCustomEmailTestHandler(t, nil)
	rec := postSettingsJSON(t, h.TestSMTPConnection, fmt.Sprintf(`{"email_provider":"custom","custom_email_base_url":%q,"custom_email_token":"ezm_t"}`, ez.URL))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "connection successful")
	auths, bodies := ez.requests()
	require.Equal(t, []string{"Bearer ezm_t"}, auths)
	require.Equal(t, []string{"{}"}, bodies)
}

func TestTestSMTPConnectionCustomProviderFailureReturns400(t *testing.T) {
	ez := newFakeEzEmail(t, 401, `{"code":"unauthorized"}`)
	h := newCustomEmailTestHandler(t, nil)
	rec := postSettingsJSON(t, h.TestSMTPConnection, fmt.Sprintf(`{"email_provider":"custom","custom_email_base_url":%q,"custom_email_token":"ezm_t"}`, ez.URL))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "access token was rejected")
}

func TestTestSMTPConnectionCustomProviderUsesSavedTokenForSavedURL(t *testing.T) {
	ez := newFakeEzEmail(t, 400, `{"code":"invalid_request"}`)
	h := newCustomEmailTestHandler(t, map[string]string{
		service.SettingKeyCustomEmailBaseURL: ez.URL,
		service.SettingKeyCustomEmailToken:   "ezm_saved",
	})
	rec := postSettingsJSON(t, h.TestSMTPConnection, `{"email_provider":"custom"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	auths, _ := ez.requests()
	require.Equal(t, []string{"Bearer ezm_saved"}, auths)
}

func TestTestSMTPConnectionCustomProviderRejectsSavedTokenForNewURL(t *testing.T) {
	ez := newFakeEzEmail(t, 400, `{"code":"invalid_request"}`)
	h := newCustomEmailTestHandler(t, map[string]string{
		service.SettingKeyCustomEmailBaseURL: "http://saved.invalid",
		service.SettingKeyCustomEmailToken:   "ezm_saved",
	})
	rec := postSettingsJSON(t, h.TestSMTPConnection, fmt.Sprintf(`{"email_provider":"custom","custom_email_base_url":%q}`, ez.URL))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "is required when testing an unsaved")
	auths, _ := ez.requests()
	require.Empty(t, auths)
}

func TestTestSMTPConnectionCustomProviderTrimsTypedToken(t *testing.T) {
	ez := newFakeEzEmail(t, 400, `{"code":"invalid_request"}`)
	h := newCustomEmailTestHandler(t, nil)
	rec := postSettingsJSON(t, h.TestSMTPConnection, fmt.Sprintf(`{"email_provider":"custom","custom_email_base_url":%q,"custom_email_token":" ezm_typed\n"}`, ez.URL))
	require.Equal(t, http.StatusOK, rec.Code)
	auths, _ := ez.requests()
	require.Equal(t, []string{"Bearer ezm_typed"}, auths)
}

func TestSendTestEmailCustomProviderReturnsDeliveryInfo(t *testing.T) {
	ez := newFakeEzEmail(t, 200, `{"id":7,"status":"sent","from":"noreply@example.com","message_id":"<m@example.com>"}`)
	h := newCustomEmailTestHandler(t, nil)
	rec := postSettingsJSON(t, h.SendTestEmail, fmt.Sprintf(`{"email":"admin@example.com","email_provider":"custom","custom_email_base_url":%q,"custom_email_token":"ezm_t"}`, ez.URL))
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "<m@example.com>", resp.Data["message_id"])
	require.Equal(t, "noreply@example.com", resp.Data["from"])
	_, bodies := ez.requests()
	require.Len(t, bodies, 1)
	var sent struct {
		To []string `json:"to"`
	}
	require.NoError(t, json.Unmarshal([]byte(bodies[0]), &sent))
	require.Equal(t, []string{"admin@example.com"}, sent.To)
}

func TestTestSMTPConnectionWithoutProviderKeepsSMTPPath(t *testing.T) {
	h := newCustomEmailTestHandler(t, nil)
	rec := postSettingsJSON(t, h.TestSMTPConnection, `{}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "SMTP host is required")
}
