//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func customEmailSettings(url string) map[string]string {
	return map[string]string{
		SettingKeyEmailProvider:      EmailProviderCustom,
		SettingKeyCustomEmailBaseURL: url,
		SettingKeyCustomEmailToken:   "ezm_test",
	}
}

func TestSendEmailUsesCustomProviderWhenSelected(t *testing.T) {
	ctx := context.Background()
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, fakeEZEmailOK })
	smtpServer := startNotificationEmailTestSMTPServer(t)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(ctx, smtpServer.settings()))
	require.NoError(t, repo.SetMultiple(ctx, customEmailSettings(ez.URL())))

	require.NoError(t, NewEmailService(repo, nil).SendEmail(ctx, "user@example.com", "s", "<p>b</p>"))
	require.Len(t, ez.Requests(), 1)
	require.Zero(t, smtpServer.messageCount())
}

func TestSendEmailUsesSMTPWhenProviderMissingOrSMTP(t *testing.T) {
	cases := map[string]bool{
		"provider missing":                 false,
		"provider smtp with custom config": true,
	}
	for name, withCustom := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, fakeEZEmailOK })
			smtpServer := startNotificationEmailTestSMTPServer(t)
			repo := newNotificationEmailMemorySettingRepo()
			require.NoError(t, repo.SetMultiple(ctx, smtpServer.settings()))
			if withCustom {
				custom := customEmailSettings(ez.URL())
				custom[SettingKeyEmailProvider] = EmailProviderSMTP
				require.NoError(t, repo.SetMultiple(ctx, custom))
			}

			require.NoError(t, NewEmailService(repo, nil).SendEmail(ctx, "user@example.com", "s", "<p>b</p>"))
			require.EqualValues(t, 1, smtpServer.messageCount())
			require.Empty(t, ez.Requests())
		})
	}
}

func TestSendEmailCustomProviderIncompleteIsNotConfigured(t *testing.T) {
	ctx := context.Background()
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, fakeEZEmailOK })
	smtpServer := startNotificationEmailTestSMTPServer(t)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(ctx, smtpServer.settings()))
	require.NoError(t, repo.SetMultiple(ctx, map[string]string{
		SettingKeyEmailProvider:      EmailProviderCustom,
		SettingKeyCustomEmailBaseURL: ez.URL(),
	}))

	err := NewEmailService(repo, nil).SendEmail(ctx, "user@example.com", "s", "<p>b</p>")
	require.ErrorIs(t, err, ErrEmailNotConfigured)
	require.Zero(t, smtpServer.messageCount())
	require.Empty(t, ez.Requests())
}

func TestSendEmailUnknownProviderFailsClosed(t *testing.T) {
	ctx := context.Background()
	smtpServer := startNotificationEmailTestSMTPServer(t)
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.SetMultiple(ctx, smtpServer.settings()))
	require.NoError(t, repo.SetMultiple(ctx, map[string]string{SettingKeyEmailProvider: "bogus"}))

	err := NewEmailService(repo, nil).SendEmail(ctx, "user@example.com", "s", "<p>b</p>")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrEmailNotConfigured)
	require.Zero(t, smtpServer.messageCount())
}

func TestSendEmailProviderReadErrorFailsClosed(t *testing.T) {
	smtpServer := startNotificationEmailTestSMTPServer(t)
	repo := &subscriptionExpirySettingRepoStub{values: smtpServer.settings(), multiErr: errors.New("db down")}

	err := NewEmailService(repo, nil).SendEmail(context.Background(), "user@example.com", "s", "<p>b</p>")
	require.Error(t, err)
	require.Zero(t, smtpServer.messageCount())
}

func TestEnsureEmailConfigured(t *testing.T) {
	tests := []struct {
		name     string
		values   map[string]string
		wantErr  bool
		wantNotC bool
	}{
		{name: "empty", values: map[string]string{}, wantErr: true, wantNotC: true},
		{name: "smtp host", values: map[string]string{SettingKeySMTPHost: "smtp.example.com"}},
		{name: "custom complete", values: customEmailSettings("http://ez.local")},
		{name: "custom without token", values: map[string]string{
			SettingKeyEmailProvider:      EmailProviderCustom,
			SettingKeyCustomEmailBaseURL: "http://ez.local",
		}, wantErr: true, wantNotC: true},
		{name: "unknown provider", values: map[string]string{
			SettingKeyEmailProvider: "bogus",
			SettingKeySMTPHost:      "smtp.example.com",
		}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewEmailService(&subscriptionExpirySettingRepoStub{values: tt.values}, nil)
			err := svc.EnsureEmailConfigured(context.Background())
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tt.wantNotC {
				require.ErrorIs(t, err, ErrEmailNotConfigured)
			} else {
				require.NotErrorIs(t, err, ErrEmailNotConfigured)
			}
		})
	}
}

func newExpiryServiceWithSettings(values map[string]string) (*SubscriptionExpiryService, *subscriptionExpiryRepoStub) {
	repo := &subscriptionExpiryRepoStub{}
	settingRepo := &subscriptionExpirySettingRepoStub{values: values}
	svc := NewSubscriptionExpiryService(repo, time.Minute)
	svc.SetSettingRepository(settingRepo)
	svc.SetNotificationEmailService(NewNotificationEmailService(settingRepo, NewEmailService(settingRepo, nil)))
	svc.SetLeaderLock(&fakeLeaderLockCache{}, nil)
	return svc, repo
}

func TestSubscriptionExpiryService_CustomEmailProviderCountsAsConfigured(t *testing.T) {
	svc, repo := newExpiryServiceWithSettings(customEmailSettings("http://ez.local"))

	svc.sendExpiryReminders(context.Background())

	require.Equal(t, 1, repo.listCalls)
}

func TestSubscriptionExpiryService_UnknownEmailProviderSkipsReminders(t *testing.T) {
	svc, repo := newExpiryServiceWithSettings(map[string]string{
		SettingKeyEmailProvider: "bogus",
		SettingKeySMTPHost:      "smtp.example.com",
	})

	svc.sendExpiryReminders(context.Background())

	require.Zero(t, repo.listCalls)
}
