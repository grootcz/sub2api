//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// ctxAwareSettingRepo 模拟会遵守 ctx 的数据库驱动：ctx 已结束时 Set 直接失败。
type ctxAwareSettingRepo struct {
	*notificationEmailMemorySettingRepo
}

func (r ctxAwareSettingRepo) Set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.notificationEmailMemorySettingRepo.Set(ctx, key, value)
}

func TestNotificationEmailSendRecordsDeliveryAfterCallerContextCanceled(t *testing.T) {
	repo := ctxAwareSettingRepo{newNotificationEmailMemorySettingRepo()}
	smtpServer := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(context.Background(), smtpServer.settings()))
	svc := NewNotificationEmailService(repo, NewEmailService(repo, nil))
	input := NotificationEmailSendInput{
		Event:          NotificationEmailEventSubscriptionExpiryReminder,
		RecipientEmail: "User@Example.com",
		RecipientName:  "User",
		UserID:         42,
		SourceType:     "user_subscription",
		SourceID:       "1234567890",
		ReminderKey:    "7d",
		Variables: map[string]string{
			"subscription_group": "Codex",
			"expiry_time":        "2026-05-27 12:00",
			"days_remaining":     "7",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, svc.Send(ctx, input))
	require.Equal(t, int64(1), smtpServer.messageCount())
	key := notificationEmailDeliveryKey(input.Event, input.SourceType, input.SourceID, input.RecipientEmail, input.ReminderKey)
	_, err := repo.GetValue(context.Background(), key)
	require.NoError(t, err)

	require.NoError(t, svc.Send(context.Background(), input))
	require.Equal(t, int64(1), smtpServer.messageCount())
}
