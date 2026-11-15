package service

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"unicode"
)

const (
	EmailProviderSMTP                = "smtp"
	EmailProviderCustom              = "custom"
	CustomEmailDefaultTimeoutSeconds = 45
	CustomEmailMinTimeoutSeconds     = 10
	CustomEmailMaxTimeoutSeconds     = 300

	customEmailSubjectMaxRunes = 200
	customEmailEmptySubject    = "(no subject)"
)

// CustomEmailConfig 自定义邮件服务（ez-email）的运行时配置。
type CustomEmailConfig struct {
	BaseURL        string
	Token          string
	From           string
	TimeoutSeconds int
}

// ParseEmailProvider 规范化发送方式；只有 smtp/custom 返回 true。
func ParseEmailProvider(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == EmailProviderSMTP || v == EmailProviderCustom {
		return v, true
	}
	return "", false
}

// resolveStoredEmailProvider 解析已存储的发送方式：空串按 smtp，未知值报错。
func resolveStoredEmailProvider(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return EmailProviderSMTP, nil
	}
	if v, ok := ParseEmailProvider(raw); ok {
		return v, nil
	}
	return "", fmt.Errorf("unknown email_provider %q", raw)
}

// NormalizeCustomEmailBaseURL 校验并规范化 base URL（去首尾空格与末尾斜杠）。
func NormalizeCustomEmailBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.Contains(trimmed, "#") {
		return "", errors.New("base URL must not contain a fragment")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("base URL scheme must be http or https")
	}
	if u.Host == "" || u.Opaque != "" {
		return "", errors.New("base URL must include a host")
	}
	if u.User != nil {
		return "", errors.New("base URL must not contain credentials")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", errors.New("base URL must not contain a query")
	}
	return strings.TrimRight(trimmed, "/"), nil
}

// NormalizeCustomEmailFrom 校验发件人并返回纯地址。
func NormalizeCustomEmailFrom(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", errors.New("from address must not contain line breaks")
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid from address: %w", err)
	}
	return addr.Address, nil
}

// NormalizeCustomEmailTimeoutSeconds 将单次调用超时限制在合法范围内。
func NormalizeCustomEmailTimeoutSeconds(seconds int) int {
	if seconds <= 0 {
		return CustomEmailDefaultTimeoutSeconds
	}
	if seconds < CustomEmailMinTimeoutSeconds {
		return CustomEmailMinTimeoutSeconds
	}
	if seconds > CustomEmailMaxTimeoutSeconds {
		return CustomEmailMaxTimeoutSeconds
	}
	return seconds
}

// sanitizeCustomEmailSubject 去掉控制字符与首尾空格，最多保留 200 个 rune。
func sanitizeCustomEmailSubject(subject string) string {
	cleaned := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, subject))
	if cleaned == "" {
		return customEmailEmptySubject
	}
	if r := []rune(cleaned); len(r) > customEmailSubjectMaxRunes {
		cleaned = string(r[:customEmailSubjectMaxRunes])
	}
	return cleaned
}
