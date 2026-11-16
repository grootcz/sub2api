//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingServiceInitializeDefaultsIncludeEmailProvider(t *testing.T) {
	repo := newNotificationEmailMemorySettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))

	require.Equal(t, "smtp", repo.values[SettingKeyEmailProvider])
	require.Equal(t, "45", repo.values[SettingKeyCustomEmailTimeoutSeconds])
}

func TestSettingServiceParsesCustomEmailSettings(t *testing.T) {
	repo := &settingGetAllRepoStub{values: map[string]string{
		SettingKeyEmailProvider:             "custom",
		SettingKeyCustomEmailBaseURL:        "http://10.0.0.5:8081",
		SettingKeyCustomEmailToken:          "ezm_x",
		SettingKeyCustomEmailFrom:           "billing@example.com",
		SettingKeyCustomEmailTimeoutSeconds: "60",
	}}
	svc := NewSettingService(repo, &config.Config{})

	got, err := svc.GetAllSettings(context.Background())
	require.NoError(t, err)

	require.Equal(t, "custom", got.EmailProvider)
	require.Equal(t, "http://10.0.0.5:8081", got.CustomEmailBaseURL)
	require.Equal(t, "billing@example.com", got.CustomEmailFrom)
	require.Equal(t, 60, got.CustomEmailTimeoutSeconds)
	require.True(t, got.CustomEmailTokenConfigured)
	require.Equal(t, "", got.CustomEmailToken)
}

func TestSettingServiceParsesCustomEmailFallbacks(t *testing.T) {
	tests := []struct {
		name           string
		values         map[string]string
		wantProvider   string
		wantTimeout    int
		wantConfigured bool
	}{
		{name: "empty", values: map[string]string{}, wantProvider: "smtp", wantTimeout: 45, wantConfigured: false},
		{
			name: "bogus provider and bad timeout",
			values: map[string]string{
				SettingKeyEmailProvider:             "bogus",
				SettingKeyCustomEmailTimeoutSeconds: "abc",
			},
			wantProvider: "smtp", wantTimeout: 45,
		},
		{
			name:         "timeout below minimum",
			values:       map[string]string{SettingKeyCustomEmailTimeoutSeconds: "5"},
			wantProvider: "smtp", wantTimeout: 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewSettingService(&settingGetAllRepoStub{values: tt.values}, &config.Config{})
			got, err := svc.GetAllSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, tt.wantProvider, got.EmailProvider)
			require.Equal(t, tt.wantTimeout, got.CustomEmailTimeoutSeconds)
			require.Equal(t, tt.wantConfigured, got.CustomEmailTokenConfigured)
		})
	}
}

func TestSettingServiceUpdateWritesCustomEmailSettings(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		EmailProvider:             "custom",
		CustomEmailBaseURL:        "http://10.0.0.5:8081",
		CustomEmailToken:          "ezm_new",
		CustomEmailFrom:           "billing@example.com",
		CustomEmailTimeoutSeconds: 60,
	})
	require.NoError(t, err)

	require.Equal(t, "custom", repo.updates[SettingKeyEmailProvider])
	require.Equal(t, "http://10.0.0.5:8081", repo.updates[SettingKeyCustomEmailBaseURL])
	require.Equal(t, "ezm_new", repo.updates[SettingKeyCustomEmailToken])
	require.Equal(t, "billing@example.com", repo.updates[SettingKeyCustomEmailFrom])
	require.Equal(t, "60", repo.updates[SettingKeyCustomEmailTimeoutSeconds])
}

func TestSettingServiceUpdateCustomEmailGuards(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		EmailProvider:             "",
		CustomEmailToken:          "  ",
		CustomEmailTimeoutSeconds: 0,
	})
	require.NoError(t, err)

	require.NotContains(t, repo.updates, SettingKeyCustomEmailToken)
	require.NotContains(t, repo.updates, SettingKeyEmailProvider)
	require.Equal(t, "45", repo.updates[SettingKeyCustomEmailTimeoutSeconds])
}
