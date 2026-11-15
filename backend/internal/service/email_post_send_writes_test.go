//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ctxAwareEmailCache is an EmailCache whose write methods honor ctx cancellation.
type ctxAwareEmailCache struct {
	mu         sync.Mutex
	notifyCode map[string]*VerificationCodeData
	cooldowns  map[string]bool
	userRate   map[int64]int64
}

func newCtxAwareEmailCache() *ctxAwareEmailCache {
	return &ctxAwareEmailCache{
		notifyCode: map[string]*VerificationCodeData{},
		cooldowns:  map[string]bool{},
		userRate:   map[int64]int64{},
	}
}

func (c *ctxAwareEmailCache) GetVerificationCode(context.Context, string) (*VerificationCodeData, error) {
	return nil, ErrSettingNotFound
}
func (c *ctxAwareEmailCache) SetVerificationCode(context.Context, string, *VerificationCodeData, time.Duration) error {
	return nil
}
func (c *ctxAwareEmailCache) DeleteVerificationCode(context.Context, string) error { return nil }
func (c *ctxAwareEmailCache) IncrVerificationCodeAttempts(context.Context, string) (int, error) {
	return 0, nil
}
func (c *ctxAwareEmailCache) GetNotifyVerifyCode(_ context.Context, email string) (*VerificationCodeData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := c.notifyCode[email]; ok {
		return d, nil
	}
	return nil, ErrSettingNotFound
}
func (c *ctxAwareEmailCache) SetNotifyVerifyCode(ctx context.Context, email string, data *VerificationCodeData, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifyCode[email] = data
	return nil
}
func (c *ctxAwareEmailCache) DeleteNotifyVerifyCode(context.Context, string) error { return nil }
func (c *ctxAwareEmailCache) IncrNotifyVerifyCodeAttempts(context.Context, string) (int, error) {
	return 0, nil
}
func (c *ctxAwareEmailCache) GetPasswordResetToken(context.Context, string) (*PasswordResetTokenData, error) {
	return nil, ErrSettingNotFound
}
func (c *ctxAwareEmailCache) SetPasswordResetToken(context.Context, string, *PasswordResetTokenData, time.Duration) error {
	return nil
}
func (c *ctxAwareEmailCache) DeletePasswordResetToken(context.Context, string) error { return nil }
func (c *ctxAwareEmailCache) ConsumePasswordResetToken(context.Context, string, string) (bool, error) {
	return false, nil
}
func (c *ctxAwareEmailCache) IsPasswordResetEmailInCooldown(_ context.Context, email string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cooldowns[email]
}
func (c *ctxAwareEmailCache) SetPasswordResetEmailCooldown(ctx context.Context, email string, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cooldowns[email] = true
	return nil
}
func (c *ctxAwareEmailCache) IncrNotifyCodeUserRate(ctx context.Context, userID int64, _ time.Duration) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userRate[userID]++
	return c.userRate[userID], nil
}
func (c *ctxAwareEmailCache) GetNotifyCodeUserRate(_ context.Context, userID int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userRate[userID], nil
}

// newCancelDuringSendEmailService returns an EmailService (custom provider) whose fake ez-email
// cancels the caller's ctx while the send is in flight, then answers 200.
func newCancelDuringSendEmailService(t *testing.T, cache EmailCache, cancel context.CancelFunc) *EmailService {
	t.Helper()
	fake := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) {
		cancel()
		return 200, fakeEZEmailOK
	})
	repo := newNotificationEmailMemorySettingRepo()
	require.NoError(t, repo.Set(context.Background(), SettingKeyEmailProvider, EmailProviderCustom))
	require.NoError(t, repo.Set(context.Background(), SettingKeyCustomEmailBaseURL, fake.URL()))
	require.NoError(t, repo.Set(context.Background(), SettingKeyCustomEmailToken, "ezm_test"))
	return NewEmailService(repo, cache)
}

func TestSendNotifyEmailCode_PersistsStateWhenCallerCtxCancelledDuringSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := newCtxAwareEmailCache()
	emailService := newCancelDuringSendEmailService(t, cache, cancel)

	svc := &UserService{}
	err := svc.SendNotifyEmailCode(ctx, 7, "notify@example.com", emailService, cache)

	require.NoError(t, err)
	require.Error(t, ctx.Err(), "caller ctx must have been cancelled during send")
	data, getErr := cache.GetNotifyVerifyCode(context.Background(), "notify@example.com")
	require.NoError(t, getErr)
	require.NotEmpty(t, data.Code)
	rate, _ := cache.GetNotifyCodeUserRate(context.Background(), 7)
	require.EqualValues(t, 1, rate)
}

func TestSendPasswordResetEmailWithCooldown_SetsCooldownWhenCallerCtxCancelledDuringSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := newCtxAwareEmailCache()
	emailService := newCancelDuringSendEmailService(t, cache, cancel)

	err := emailService.SendPasswordResetEmailWithCooldown(ctx, "user@example.com", "Sub2API", "https://example.com/reset")

	require.NoError(t, err)
	require.Error(t, ctx.Err(), "caller ctx must have been cancelled during send")
	require.True(t, cache.IsPasswordResetEmailInCooldown(context.Background(), "user@example.com"))
}

// ctxAwareModerationRepo fails CreateLog when ctx is done.
type ctxAwareModerationRepo struct {
	contentModerationTestRepo
}

func (r *ctxAwareModerationRepo) CreateLog(ctx context.Context, log *ContentModerationLog) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.contentModerationTestRepo.CreateLog(ctx, log)
}

func TestPersistContentModerationLog_WritesLogWhenCallerCtxAlreadyCancelled(t *testing.T) {
	repo := &ctxAwareModerationRepo{}
	svc := &ContentModerationService{repo: repo}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	svc.persistContentModerationLog(ctx, nil, &ContentModerationLog{Endpoint: "/v1/chat/completions", Flagged: true}, "", false, false)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.logs, 1)
}
