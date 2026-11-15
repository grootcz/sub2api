package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
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
