//go:build unit

package admin

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

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
	stored := storedCustomEmail()
	h, repo := newStepUpSwitchTestHandler(t, stored)
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
		h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
		rec := doUpdateSettings(t, h, map[string]any{"email_provider": v}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, "%v", v)
		require.Contains(t, rec.Body.String(), "email_provider must be one of")
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
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
		rec := doUpdateSettings(t, h, map[string]any{"custom_email_timeout_seconds": tc.in}, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, tc.want, repo.values[service.SettingKeyCustomEmailTimeoutSeconds], "in=%d", tc.in)
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
