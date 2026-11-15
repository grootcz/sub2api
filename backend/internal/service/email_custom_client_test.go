//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeEZEmailRequest struct {
	Path   string
	Header http.Header
	Body   string
}

type fakeEZEmail struct {
	server *httptest.Server
	mu     sync.Mutex
	reqs   []fakeEZEmailRequest
}

// startFakeEZEmail 启动模拟 ez-email：respond 收到从 1 开始的调用序号和本次请求，返回状态码和响应体。
func startFakeEZEmail(t *testing.T, respond func(n int, r fakeEZEmailRequest) (int, string)) *fakeEZEmail {
	t.Helper()
	f := &fakeEZEmail{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := fakeEZEmailRequest{Path: r.URL.Path, Header: r.Header.Clone(), Body: string(body)}
		f.mu.Lock()
		f.reqs = append(f.reqs, req)
		n := len(f.reqs)
		f.mu.Unlock()
		status, resp := respond(n, req)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeEZEmail) URL() string { return f.server.URL }

func (f *fakeEZEmail) Requests() []fakeEZEmailRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeEZEmailRequest(nil), f.reqs...)
}

const fakeEZEmailOK = `{"id":1234,"status":"sent","from":"noreply@example.com","message_id":"<abc@example.com>"}`

// newTestEZEmailClient 基于 newEZEmailClient()：退避缩短、无抖动、超时单位改为毫秒。
func newTestEZEmailClient() *ezEmailClient {
	c := newEZEmailClient()
	c.retryBackoff = []time.Duration{time.Millisecond, 2 * time.Millisecond}
	c.maxJitter = 0
	c.timeoutUnit = time.Millisecond
	return c
}

func ezCfg(url string) CustomEmailConfig {
	return CustomEmailConfig{BaseURL: url, Token: "ezm_test", TimeoutSeconds: 300}
}

func asCustomEmailError(t *testing.T, err error) *CustomEmailError {
	t.Helper()
	var ce *CustomEmailError
	require.True(t, errors.As(err, &ce), "expected *CustomEmailError, got %v", err)
	return ce
}

func closedLocalAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func TestEZEmailSendBuildsRequest(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, fakeEZEmailOK })
	res, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()+"/prefix/"),
		"User <user@example.com>", "主题\t", "<p>hi</p>")
	require.NoError(t, err)
	require.Equal(t, int64(1234), res.ID)
	require.Equal(t, "<abc@example.com>", res.MessageID)
	req := ez.Requests()[0]
	require.Equal(t, "/prefix/api/v1/send", req.Path)
	require.Equal(t, "Bearer ezm_test", req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Regexp(t, `^[0-9a-f]{32}$`, req.Header.Get("X-Request-ID"))
	require.JSONEq(t, `{"to":["user@example.com"],"subject":"主题","html":"<p>hi</p>"}`, req.Body)
}

func TestEZEmailSendIncludesPreferredFrom(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, fakeEZEmailOK })
	cfg := ezCfg(ez.URL())
	cfg.From = "billing@example.com"
	_, err := newTestEZEmailClient().Send(context.Background(), cfg, "user@example.com", "s", "<p>hi</p>")
	require.NoError(t, err)
	require.JSONEq(t, `{"to":["user@example.com"],"subject":"s","html":"<p>hi</p>","from":"billing@example.com"}`, ez.Requests()[0].Body)
}

func TestEZEmailSendRejectsInvalidRecipientWithoutCalling(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, fakeEZEmailOK })
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "not-an-email", "s", "b")
	require.Error(t, err)
	require.Equal(t, CustomEmailOutcomeNotSent, asCustomEmailError(t, err).Outcome)
	require.Len(t, ez.Requests(), 0)
	require.Contains(t, err.Error(), "invalid recipient address")
	require.Contains(t, err.Error(), "email was not sent")
	require.NotContains(t, err.Error(), "SMTP")
}

func TestEZEmailSendRetriesBusyThenSucceeds(t *testing.T) {
	ez := startFakeEZEmail(t, func(n int, _ fakeEZEmailRequest) (int, string) {
		if n <= 2 {
			return 503, `{"code":"busy"}`
		}
		return 200, fakeEZEmailOK
	})
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
	require.NoError(t, err)
	reqs := ez.Requests()
	require.Len(t, reqs, 3)
	id := reqs[0].Header.Get("X-Request-ID")
	require.NotEmpty(t, id)
	require.Equal(t, id, reqs[1].Header.Get("X-Request-ID"))
	require.Equal(t, id, reqs[2].Header.Get("X-Request-ID"))
}

func TestEZEmailSendGivesUpAfterTwoBusyRetries(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 503, `{"code":"busy"}` })
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
	ce := asCustomEmailError(t, err)
	require.Equal(t, "busy", ce.Code)
	require.Equal(t, CustomEmailOutcomeNotSent, ce.Outcome)
	require.Len(t, ez.Requests(), 3)
}

func TestEZEmailSendStopsRetryingWhenCallerContextDone(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 503, `{"code":"busy"}` })
	c := newTestEZEmailClient()
	c.retryBackoff = []time.Duration{time.Second, time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Send(ctx, ezCfg(ez.URL()), "user@example.com", "s", "b")
	ce := asCustomEmailError(t, err)
	require.Equal(t, "busy", ce.Code)
	require.Equal(t, CustomEmailOutcomeNotSent, ce.Outcome)
	require.Len(t, ez.Requests(), 1)
	require.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestEZEmailSendDoesNotRetryOtherFailures(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		outcome CustomEmailOutcome
		contain []string
	}{
		{"504", 504, `{"code":"send_timeout"}`, CustomEmailOutcomeUnknown,
			[]string{"result unknown, the email may have been sent"}},
		{"502 with hint", 502,
			`{"code":"send_failed","error":"发送失败","detail":"535 rejected","hint":"Gmail 拒绝了用户名或应用专用密码","request_id":"order-1","id":1252}`,
			CustomEmailOutcomeNotSent,
			[]string{"HTTP 502", "send_failed", "535 rejected", "Gmail 拒绝了用户名或应用专用密码", "request_id=order-1", "id=1252", "email was not sent"}},
		{"502 without hint", 502, `{"code":"send_failed","detail":"connection reset"}`, CustomEmailOutcomeUnknown, nil},
		{"422", 422, `{"code":"no_mailbox"}`, CustomEmailOutcomeNotSent, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return tc.status, tc.body })
			_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
			ce := asCustomEmailError(t, err)
			require.Equal(t, tc.outcome, ce.Outcome)
			for _, s := range tc.contain {
				require.Contains(t, err.Error(), s)
			}
			require.Len(t, ez.Requests(), 1)
		})
	}
}

func TestEZEmailSendNonJSONErrorKeepsStatus(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 502, "<html>Bad Gateway</html>" })
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
	ce := asCustomEmailError(t, err)
	require.Equal(t, 502, ce.HTTPStatus)
	require.Equal(t, "", ce.Code)
	require.Equal(t, "", ce.RequestID)
}

func TestEZEmailSendLimitsErrorBody(t *testing.T) {
	body := `{"code":"send_failed","detail":"` + strings.Repeat("x", 100<<10) + `"}`
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 502, body })
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
	ce := asCustomEmailError(t, err)
	require.Equal(t, 502, ce.HTTPStatus)
	require.Less(t, len(err.Error()), 70<<10)
}

func TestEZEmailSendTreatsUnparsable200AsSuccess(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 200, "ok" })
	res, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestEZEmailSendDoesNotFollowRedirect(t *testing.T) {
	var mu sync.Mutex
	elsewhere := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/send":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "/elsewhere":
			mu.Lock()
			elsewhere++
			mu.Unlock()
		}
	}))
	defer srv.Close()
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(srv.URL), "user@example.com", "s", "b")
	require.Error(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 0, elsewhere)
}

func TestEZEmailSendAttemptIgnoresCanceledCallerContext(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) {
		time.Sleep(30 * time.Millisecond)
		return 200, fakeEZEmailOK
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestEZEmailClient().Send(ctx, ezCfg(ez.URL()), "user@example.com", "s", "b")
	require.NoError(t, err)
	require.Len(t, ez.Requests(), 1)
}

func TestEZEmailSendAttemptTimeoutIsUnknown(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) {
		time.Sleep(300 * time.Millisecond)
		return 200, fakeEZEmailOK
	})
	cfg := ezCfg(ez.URL())
	cfg.TimeoutSeconds = 50
	_, err := newTestEZEmailClient().Send(context.Background(), cfg, "user@example.com", "s", "b")
	require.Equal(t, CustomEmailOutcomeUnknown, asCustomEmailError(t, err).Outcome)
	require.Contains(t, err.Error(), "result unknown, the email may have been sent")
}

func TestEZEmailSendConnectionRefusedIsNotSent(t *testing.T) {
	addr := closedLocalAddr(t)
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg("http://"+addr), "user@example.com", "s", "b")
	require.Equal(t, CustomEmailOutcomeNotSent, asCustomEmailError(t, err).Outcome)
	require.Contains(t, err.Error(), "email was not sent")
}

func TestEZEmailPing(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		contain string
	}{
		{"400 ok", 400, `{"code":"invalid_request"}`, ""},
		{"400 non-json", 400, "<html>400 Bad Request</html>", "unexpected response: HTTP 400"},
		{"400 other code", 400, `{"code":"something_else"}`, "unexpected response: HTTP 400 something_else"},
		{"401 unauthorized", 401, `{"code":"unauthorized"}`, "ez-email access token was rejected (unauthorized)"},
		{"401 token_not_configured", 401, `{"code":"token_not_configured"}`, "ez-email has not generated an access token yet (token_not_configured)"},
		{"403", 403, `{"code":"forbidden_source"}`, "this server's egress IP is not in the ez-email source IP allowlist (forbidden_source)"},
		{"404", 404, `{"code":"not_found"}`, "ez-email endpoint not found, check the base URL (HTTP 404)"},
		{"503 busy", 503, `{"code":"busy"}`, "unexpected response: HTTP 503 busy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return tc.status, tc.body })
			err := newTestEZEmailClient().Ping(context.Background(), ezCfg(ez.URL()))
			reqs := ez.Requests()
			require.Len(t, reqs, 1)
			if tc.contain == "" {
				require.NoError(t, err)
				require.Equal(t, "{}", reqs[0].Body)
				require.Equal(t, "Bearer ezm_test", reqs[0].Header.Get("Authorization"))
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.contain)
		})
	}
}

func TestEZEmailPingNetworkError(t *testing.T) {
	addr := closedLocalAddr(t)
	err := newTestEZEmailClient().Ping(context.Background(), ezCfg("http://"+addr))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot reach ez-email: ")
}

func TestEZEmailSendTreatsTruncated200BodyAsSuccess(t *testing.T) {
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"sta`))
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	res, err := newTestEZEmailClient().Send(context.Background(), ezCfg(srv.URL), "user@example.com", "s", "b")
	require.NoError(t, err)
	require.NotNil(t, res)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, count)
}

func TestEZEmailSendOnlyAccepts200(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 202, `{}` })
	_, err := newTestEZEmailClient().Send(context.Background(), ezCfg(ez.URL()), "user@example.com", "s", "b")
	ce := asCustomEmailError(t, err)
	require.Equal(t, 202, ce.HTTPStatus)
	require.Equal(t, CustomEmailOutcomeNotSent, ce.Outcome)
	require.Len(t, ez.Requests(), 1)
}

func TestEZEmailSendChecksCallerContextBeforeNextAttempt(t *testing.T) {
	ez := startFakeEZEmail(t, func(int, fakeEZEmailRequest) (int, string) { return 503, `{"code":"busy"}` })
	c := newTestEZEmailClient()
	c.retryBackoff = []time.Duration{0, 0}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Send(ctx, ezCfg(ez.URL()), "user@example.com", "s", "b")
	ce := asCustomEmailError(t, err)
	require.Equal(t, "busy", ce.Code)
	require.Equal(t, CustomEmailOutcomeNotSent, ce.Outcome)
	require.Len(t, ez.Requests(), 1)
}

func TestNewEZEmailClientProductionDefaults(t *testing.T) {
	c := newEZEmailClient()
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second}, c.retryBackoff)
	require.Equal(t, 500*time.Millisecond, c.maxJitter)
	require.Equal(t, time.Second, c.timeoutUnit)
	tr, ok := c.httpClient.Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, tr.Proxy)
	require.Equal(t, 20*time.Second, tr.IdleConnTimeout)
	require.Equal(t, 5*time.Second, tr.TLSHandshakeTimeout)
	require.Equal(t, 5*time.Second, ezEmailDialTimeout)
	require.Equal(t, http.ErrUseLastResponse, c.httpClient.CheckRedirect(nil, nil))
}
