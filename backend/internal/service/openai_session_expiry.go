package service

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func resolveOpenAISessionAccessTokenExpiry(accessToken, sessionExpires string) time.Time {
	if claims, err := openai.DecodeIDToken(strings.TrimSpace(accessToken)); err == nil && claims != nil && claims.Exp > 0 {
		return time.Unix(claims.Exp, 0).UTC()
	}
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(sessionExpires)); err == nil {
		return parsed.UTC()
	}
	return time.Now().Add(time.Hour).UTC()
}

func resolveOpenAISessionAccessTokenExpiryUnix(accessToken, sessionExpires string) int64 {
	return resolveOpenAISessionAccessTokenExpiry(accessToken, sessionExpires).Unix()
}

func resolveOpenAISessionAccessTokenExpiryRFC3339(accessToken, sessionExpires string) string {
	return resolveOpenAISessionAccessTokenExpiry(accessToken, sessionExpires).Format(time.RFC3339)
}
