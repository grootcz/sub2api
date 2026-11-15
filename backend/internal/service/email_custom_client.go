package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"
)

// CustomEmailOutcome 描述一次失败的发信是否可能已经投递。
type CustomEmailOutcome string

const (
	CustomEmailOutcomeNotSent CustomEmailOutcome = "not_sent"
	CustomEmailOutcomeUnknown CustomEmailOutcome = "unknown"
)

const (
	ezEmailSendPath         = "/api/v1/send"
	ezEmailMaxResponseBytes = 64 << 10
	ezEmailDialTimeout      = 5 * time.Second
	ezEmailIdleConnTimeout  = 20 * time.Second
	ezEmailTLSTimeout       = 5 * time.Second
	ezEmailBusyCode         = "busy"
)

// CustomEmailError 是 ez-email 调用失败的结构化错误。
type CustomEmailError struct {
	Outcome    CustomEmailOutcome
	HTTPStatus int    // 网络错误时为 0
	Code       string // ez-email 的 code
	Message    string // ez-email 的 error
	Detail     string
	Hint       string
	RequestID  string
	RecordID   int64 // ez-email 的 id（发送历史记录 ID）
	Err        error // 底层网络错误
}

func (e *CustomEmailError) outcomePhrase() string {
	if e.Outcome == CustomEmailOutcomeUnknown {
		return "result unknown, the email may have been sent"
	}
	return "email was not sent"
}

// invalidRecipientError 表示收件人地址非法，请求未发出。
type invalidRecipientError struct{ reason string }

func (e *invalidRecipientError) Error() string { return e.reason }

func newInvalidRecipientError(err error) error {
	reason := "address could not be parsed"
	if inner := errors.Unwrap(err); inner != nil {
		reason = inner.Error()
	} else if strings.Contains(err.Error(), "line break") {
		reason = "address contains a line break"
	}
	return &invalidRecipientError{reason: reason}
}

func (e *CustomEmailError) Error() string {
	var inv *invalidRecipientError
	if errors.As(e.Err, &inv) {
		return fmt.Sprintf("custom email not sent: invalid recipient address: %s; %s", inv.reason, e.outcomePhrase())
	}
	if e.HTTPStatus == 0 {
		return fmt.Sprintf("custom email request failed: %v; %s", e.Err, e.outcomePhrase())
	}
	parts := []string{fmt.Sprintf("custom email service returned HTTP %d", e.HTTPStatus)}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Detail != "" {
		parts = append(parts, "detail: "+e.Detail)
	}
	if e.Hint != "" {
		parts = append(parts, "hint: "+e.Hint)
	}
	if e.RequestID != "" {
		parts = append(parts, "request_id="+e.RequestID)
	}
	if e.RecordID > 0 {
		parts = append(parts, fmt.Sprintf("id=%d", e.RecordID))
	}
	parts = append(parts, e.outcomePhrase())
	return strings.Join(parts, "; ")
}

func (e *CustomEmailError) Unwrap() error { return e.Err }

// CustomEmailSendResult 是 ez-email 发送成功的响应。
type CustomEmailSendResult struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	From      string `json:"from"`
	MessageID string `json:"message_id"`
}

type ezEmailErrorBody struct {
	Code      string `json:"code"`
	Error     string `json:"error"`
	Detail    string `json:"detail"`
	Hint      string `json:"hint"`
	RequestID string `json:"request_id"`
	ID        int64  `json:"id"`
}

type ezEmailClient struct {
	httpClient   *http.Client
	retryBackoff []time.Duration // 生产值 {1s, 2s}；长度即最大重试次数
	maxJitter    time.Duration   // 生产值 500ms
	timeoutUnit  time.Duration   // 生产值 time.Second；测试改成毫秒
}

func newEZEmailClient() *ezEmailClient {
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: ezEmailDialTimeout}).DialContext,
		IdleConnTimeout:     ezEmailIdleConnTimeout,
		TLSHandshakeTimeout: ezEmailTLSTimeout,
	}
	return &ezEmailClient{
		httpClient: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryBackoff: []time.Duration{time.Second, 2 * time.Second},
		maxJitter:    500 * time.Millisecond,
		timeoutUnit:  time.Second,
	}
}

var defaultEZEmailClient = newEZEmailClient()

// ezEmailAttempt 是单次 HTTP 尝试的结果。
type ezEmailAttempt struct {
	status  int
	body    []byte
	readErr error
	err     *CustomEmailError // 网络层错误；非 nil 时 status/body 无效
}

func newEZEmailRequestID() string {
	var b [16]byte
	// Go 1.24 起 crypto/rand.Read 不会返回错误，失败时直接终止进程。
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// attempt 发起一次请求。每次尝试的 ctx 与调用方取消解耦，只受调用超时约束。
func (c *ezEmailClient) attempt(ctx context.Context, cfg CustomEmailConfig, requestID string, payload []byte) ezEmailAttempt {
	timeout := time.Duration(NormalizeCustomEmailTimeoutSeconds(cfg.TimeoutSeconds)) * c.timeoutUnit
	attemptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	var wrote atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				wrote.Store(true)
			}
		},
	}
	attemptCtx = httptrace.WithClientTrace(attemptCtx, trace)

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/") + ezEmailSendPath
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ezEmailAttempt{err: &CustomEmailError{Outcome: CustomEmailOutcomeNotSent, Err: err}}
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", requestID)

	resp, err := c.httpClient.Do(req) //nolint:gosec // G704: 业务地址由管理员配置，信任级别同 SMTP 主机
	if err != nil {
		outcome := CustomEmailOutcomeNotSent
		if wrote.Load() {
			outcome = CustomEmailOutcomeUnknown
		}
		return ezEmailAttempt{err: &CustomEmailError{Outcome: outcome, RequestID: requestID, Err: err}}
	}
	defer func() { _ = resp.Body.Close() }()

	// 读体出错时保留已读到的部分；200 仍按成功处理（ez-email 已接收这封信）。
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, ezEmailMaxResponseBytes))
	return ezEmailAttempt{status: resp.StatusCode, body: body, readErr: readErr}
}

func ezEmailHTTPError(status int, body []byte) *CustomEmailError {
	var parsed ezEmailErrorBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		parsed = ezEmailErrorBody{}
	}
	outcome := CustomEmailOutcomeNotSent
	if status == http.StatusGatewayTimeout || (status == http.StatusBadGateway && parsed.Hint == "") {
		outcome = CustomEmailOutcomeUnknown
	}
	return &CustomEmailError{
		Outcome:    outcome,
		HTTPStatus: status,
		Code:       parsed.Code,
		Message:    parsed.Error,
		Detail:     parsed.Detail,
		Hint:       parsed.Hint,
		RequestID:  parsed.RequestID,
		RecordID:   parsed.ID,
	}
}

func (c *ezEmailClient) jitter() time.Duration {
	if c.maxJitter <= 0 {
		return 0
	}
	return mrand.N(c.maxJitter + 1)
}

// Send 通过 ez-email 发送一封邮件；仅在 503 busy 时重试。
func (c *ezEmailClient) Send(ctx context.Context, cfg CustomEmailConfig, to, subject, htmlBody string) (*CustomEmailSendResult, error) {
	addr, err := parseSMTPAddress(to, "recipient")
	if err != nil {
		return nil, &CustomEmailError{Outcome: CustomEmailOutcomeNotSent, Err: newInvalidRecipientError(err)}
	}
	reqBody := map[string]any{
		"to":      []string{addr.Address},
		"subject": sanitizeCustomEmailSubject(subject),
		"html":    htmlBody,
	}
	if from := strings.TrimSpace(cfg.From); from != "" {
		reqBody["from"] = from
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &CustomEmailError{Outcome: CustomEmailOutcomeNotSent, Err: err}
	}

	requestID := newEZEmailRequestID()
	var lastErr *CustomEmailError
	for i := 0; ; i++ {
		res := c.attempt(ctx, cfg, requestID, payload)
		if res.err != nil {
			return nil, res.err
		}
		if res.status == http.StatusOK {
			return c.parseSuccess(res.body, res.readErr, requestID, addr.Address), nil
		}
		lastErr = ezEmailHTTPError(res.status, res.body)
		if res.status != http.StatusServiceUnavailable || lastErr.Code != ezEmailBusyCode || i >= len(c.retryBackoff) {
			return nil, lastErr
		}
		timer := time.NewTimer(c.retryBackoff[i] + c.jitter())
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, lastErr
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return nil, lastErr
		}
	}
}

func (c *ezEmailClient) parseSuccess(body []byte, readErr error, requestID, recipient string) *CustomEmailSendResult {
	result := &CustomEmailSendResult{}
	err := readErr
	if err == nil {
		err = json.Unmarshal(body, result)
	}
	if err != nil {
		slog.Warn("custom email sent but response could not be parsed",
			"request_id", requestID, "recipient_hash", notificationEmailHash(recipient), "error", err)
		return &CustomEmailSendResult{}
	}
	slog.Info("custom email sent",
		"request_id", requestID,
		"id", result.ID,
		"message_id", result.MessageID,
		"from", result.From,
		"recipient_hash", notificationEmailHash(recipient))
	return result
}

// Ping 用空请求体探测连通性与令牌：只试一次，400 视为成功。
func (c *ezEmailClient) Ping(ctx context.Context, cfg CustomEmailConfig) error {
	res := c.attempt(ctx, cfg, newEZEmailRequestID(), []byte("{}"))
	if res.err != nil {
		return fmt.Errorf("cannot reach ez-email: %w", res.err.Err)
	}
	httpErr := ezEmailHTTPError(res.status, res.body)
	switch {
	case res.status == http.StatusBadRequest:
		return nil
	case res.status == http.StatusUnauthorized && httpErr.Code == "token_not_configured":
		return errors.New("ez-email has not generated an access token yet (token_not_configured)")
	case res.status == http.StatusUnauthorized:
		return errors.New("ez-email access token was rejected (unauthorized)")
	case res.status == http.StatusForbidden:
		return errors.New("this server's egress IP is not in the ez-email source IP allowlist (forbidden_source)")
	case res.status == http.StatusNotFound:
		return errors.New("ez-email endpoint not found, check the base URL (HTTP 404)")
	default:
		return fmt.Errorf("unexpected response: HTTP %d %s", res.status, httpErr.Code)
	}
}
