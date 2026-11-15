//go:build unit

package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestParseEmailProvider(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"smtp": {"smtp", true}, " Custom ": {"custom", true},
		"": {"", false}, "sendgrid": {"", false},
	}
	for raw, c := range cases {
		got, ok := ParseEmailProvider(raw)
		require.Equal(t, c.ok, ok, raw)
		require.Equal(t, c.want, got, raw)
	}
}

func TestResolveStoredEmailProvider(t *testing.T) {
	got, err := resolveStoredEmailProvider("")
	require.NoError(t, err)
	require.Equal(t, EmailProviderSMTP, got)
	got, err = resolveStoredEmailProvider(" CUSTOM ")
	require.NoError(t, err)
	require.Equal(t, EmailProviderCustom, got)
	_, err = resolveStoredEmailProvider("bogus")
	require.Error(t, err)
}

func TestNormalizeCustomEmailBaseURL(t *testing.T) {
	valid := map[string]string{
		"":                             "",
		" http://10.0.0.5:8081/ ":      "http://10.0.0.5:8081",
		"https://ez.example.com/mail/": "https://ez.example.com/mail",
		"http://[fd00::5]:8081/":       "http://[fd00::5]:8081", // Review Focus #3
	}
	for raw, want := range valid {
		got, err := NormalizeCustomEmailBaseURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"ftp://10.0.0.5", "10.0.0.5:8081", "http://", "http://h?x=1", "http://h?", "http://h#frag", "http://u:p@h"} {
		_, err := NormalizeCustomEmailBaseURL(raw)
		require.Error(t, err, raw)
	}
}

func TestNormalizeCustomEmailFrom(t *testing.T) {
	got, err := NormalizeCustomEmailFrom("")
	require.NoError(t, err)
	require.Equal(t, "", got)
	got, err = NormalizeCustomEmailFrom(" Billing <Billing@Example.com> ")
	require.NoError(t, err)
	require.Equal(t, "Billing@Example.com", got)
	for _, raw := range []string{"not-an-email", "a@b.com\r\nBcc: x@y.com"} {
		_, err := NormalizeCustomEmailFrom(raw)
		require.Error(t, err, raw)
	}
}

func TestNormalizeCustomEmailTimeoutSeconds(t *testing.T) {
	for in, want := range map[int]int{-1: 45, 0: 45, 5: 10, 10: 10, 45: 45, 300: 300, 301: 300} {
		require.Equal(t, want, NormalizeCustomEmailTimeoutSeconds(in), in)
	}
}

func TestSanitizeCustomEmailSubject(t *testing.T) {
	require.Equal(t, "abc", sanitizeCustomEmailSubject("a\tb\r\nc"))
	require.Equal(t, "hi", sanitizeCustomEmailSubject("  hi  "))
	require.Equal(t, "(no subject)", sanitizeCustomEmailSubject("\t\n "))
	long := sanitizeCustomEmailSubject(strings.Repeat("验", 201)) // Review Focus #2
	require.Equal(t, 200, utf8.RuneCountInString(long))
	require.True(t, utf8.ValidString(long))
}
