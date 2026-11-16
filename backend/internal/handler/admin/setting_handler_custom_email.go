package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// customEmailSettingsUpdate 是自定义邮件服务配置合并后的结果。
type customEmailSettingsUpdate struct {
	Provider       string
	BaseURL        string
	Token          string // 本次请求的令牌，已去空格；空串表示保留原值
	From           string
	TimeoutSeconds int
}

// resolveCustomEmailSettingsUpdate 合并请求与已保存的配置：请求带了的字段做规范化，
// 没带的字段沿用 previous；发送方式为 custom 时校验地址与令牌。
func resolveCustomEmailSettingsUpdate(req UpdateSettingsRequest, previous *service.SystemSettings, sentFields map[string]json.RawMessage) (customEmailSettingsUpdate, error) {
	out := customEmailSettingsUpdate{
		Provider:       previous.EmailProvider,
		BaseURL:        previous.CustomEmailBaseURL,
		Token:          strings.TrimSpace(req.CustomEmailToken),
		From:           previous.CustomEmailFrom,
		TimeoutSeconds: previous.CustomEmailTimeoutSeconds,
	}
	if _, sent := sentFields["email_provider"]; sent {
		provider, ok := service.ParseEmailProvider(req.EmailProvider)
		if !ok {
			return out, errors.New("email_provider must be one of: smtp, custom")
		}
		out.Provider = provider
	}
	if _, sent := sentFields["custom_email_base_url"]; sent {
		baseURL, err := service.NormalizeCustomEmailBaseURL(req.CustomEmailBaseURL)
		if err != nil {
			return out, fmt.Errorf("invalid custom_email_base_url: %w", err)
		}
		out.BaseURL = baseURL
	}
	if _, sent := sentFields["custom_email_from"]; sent {
		from, err := service.NormalizeCustomEmailFrom(req.CustomEmailFrom)
		if err != nil {
			return out, fmt.Errorf("invalid custom_email_from: %w", err)
		}
		out.From = from
	}
	if _, sent := sentFields["custom_email_timeout_seconds"]; sent {
		out.TimeoutSeconds = service.NormalizeCustomEmailTimeoutSeconds(req.CustomEmailTimeoutSeconds)
	}
	if out.Provider == service.EmailProviderCustom && (out.BaseURL == "" || (out.Token == "" && !previous.CustomEmailTokenConfigured)) {
		return out, errors.New("custom_email_base_url and custom_email_token are required when email_provider is custom")
	}
	return out, nil
}

// CustomEmailTestFields 是「测试连接」与「发送测试邮件」请求中自定义邮件服务的字段。
type CustomEmailTestFields struct {
	EmailProvider             string `json:"email_provider"`
	CustomEmailBaseURL        string `json:"custom_email_base_url"`
	CustomEmailToken          string `json:"custom_email_token"`
	CustomEmailFrom           string `json:"custom_email_from"`
	CustomEmailTimeoutSeconds int    `json:"custom_email_timeout_seconds"`
}

func (f CustomEmailTestFields) wantsCustomProvider() bool {
	provider, ok := service.ParseEmailProvider(f.EmailProvider)
	return ok && provider == service.EmailProviderCustom
}

// resolveCustomEmailTestConfig 用请求值覆盖已保存的配置；不会把已保存的令牌发往未保存的地址。
func (h *SettingHandler) resolveCustomEmailTestConfig(ctx context.Context, f CustomEmailTestFields) (service.CustomEmailConfig, error) {
	saved, err := h.emailService.GetCustomEmailConfig(ctx)
	if err != nil {
		return service.CustomEmailConfig{}, fmt.Errorf("read saved custom email settings failed: %w", err)
	}

	cfg := service.CustomEmailConfig{}
	requestedURL := strings.TrimSpace(f.CustomEmailBaseURL)
	urlIsSaved := requestedURL == ""
	if urlIsSaved {
		cfg.BaseURL = saved.BaseURL
	} else {
		baseURL, err := service.NormalizeCustomEmailBaseURL(requestedURL)
		if err != nil {
			return service.CustomEmailConfig{}, fmt.Errorf("invalid custom_email_base_url: %w", err)
		}
		cfg.BaseURL = baseURL
		urlIsSaved = baseURL == saved.BaseURL
	}

	cfg.Token = strings.TrimSpace(f.CustomEmailToken)
	if cfg.Token == "" {
		if !urlIsSaved {
			return service.CustomEmailConfig{}, errors.New("custom_email_token is required when testing an unsaved custom_email_base_url")
		}
		cfg.Token = saved.Token
	}

	from, err := service.NormalizeCustomEmailFrom(f.CustomEmailFrom)
	if err != nil {
		return service.CustomEmailConfig{}, fmt.Errorf("invalid custom_email_from: %w", err)
	}
	cfg.From = from

	timeout := f.CustomEmailTimeoutSeconds
	if timeout <= 0 {
		timeout = saved.TimeoutSeconds
	}
	cfg.TimeoutSeconds = service.NormalizeCustomEmailTimeoutSeconds(timeout)

	if cfg.BaseURL == "" {
		return service.CustomEmailConfig{}, errors.New("custom_email_base_url is required")
	}
	if cfg.Token == "" {
		return service.CustomEmailConfig{}, errors.New("custom_email_token is required")
	}
	return cfg, nil
}

// testCustomEmailConnection 处理自定义邮件服务的「测试连接」，所有失败返回 400。
func (h *SettingHandler) testCustomEmailConnection(c *gin.Context, f CustomEmailTestFields) {
	cfg, err := h.resolveCustomEmailTestConfig(c.Request.Context(), f)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if err := h.emailService.TestCustomEmailConnection(c.Request.Context(), cfg); err != nil {
		response.BadRequest(c, "Custom email connection test failed: "+err.Error())
		return
	}
	response.Success(c, gin.H{"message": "Custom email service connection successful"})
}

// sendCustomTestEmail 处理自定义邮件服务的「发送测试邮件」，所有失败返回 400。
func (h *SettingHandler) sendCustomTestEmail(c *gin.Context, to string, f CustomEmailTestFields) {
	cfg, err := h.resolveCustomEmailTestConfig(c.Request.Context(), f)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	subject, body := buildTestEmailContent(h.settingService.GetSiteName(c.Request.Context()))
	r, err := h.emailService.SendCustomEmailWithConfig(c.Request.Context(), cfg, to, subject, body)
	if err != nil {
		response.BadRequest(c, "Failed to send test email: "+err.Error())
		return
	}
	response.Success(c, gin.H{
		"message":    fmt.Sprintf("Test email sent via custom email service (from: %s, message_id: %s)", r.From, r.MessageID),
		"from":       r.From,
		"message_id": r.MessageID,
		"id":         r.ID,
	})
}
