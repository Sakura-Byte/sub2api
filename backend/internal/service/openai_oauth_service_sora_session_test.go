package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func makeUnsignedJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return header + "." + body + ".sig"
}

func TestOpenAIOAuthService_ExchangeOpenAISessionToken_UsesAccessTokenExp(t *testing.T) {
	exp := time.Now().Add(20 * time.Minute).Unix()
	accessToken := makeUnsignedJWT(fmt.Sprintf(
		`{"exp":%d,"email":"demo@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acc-1","chatgpt_user_id":"user-1","chatgpt_plan_type":"plus"}}`,
		exp,
	))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=st-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"accessToken":"%s","sessionToken":"server-st","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"},"account":{"id":"acc-1","planType":"plus"}}`, accessToken)))
	}))
	defer server.Close()

	origin := openAIChatGPTSessionAuthURL
	openAIChatGPTSessionAuthURL = server.URL
	defer func() { openAIChatGPTSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	info, err := svc.ExchangeOpenAISessionToken(context.Background(), "st-token", nil)
	require.NoError(t, err)
	require.Equal(t, exp, info.ExpiresAt)
	require.Equal(t, "server-st", info.SessionToken)
	require.Equal(t, "plus", info.PlanType)
	require.Equal(t, "acc-1", info.ChatGPTAccountID)
}

func TestOpenAIOAuthService_RefreshAccountToken_PrefersSessionTokenForOpenAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=st-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"session-at","sessionToken":"server-st","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"},"account":{"id":"acc-1","planType":"plus"}}`))
	}))
	defer server.Close()

	origin := openAIChatGPTSessionAuthURL
	openAIChatGPTSessionAuthURL = server.URL
	defer func() { openAIChatGPTSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"session_token": "st-token",
			"refresh_token": "rt-should-not-be-used",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "session-at", info.AccessToken)
	require.Equal(t, "server-st", info.SessionToken)
}

func TestOpenAIOAuthService_RefreshAccountToken_UsesRefreshTokenWhenSessionTokenMissing(t *testing.T) {
	oauthClient := &openaiOAuthClientTrackingStub{
		tokenResponse: &openai.TokenResponse{
			AccessToken:  "rt-access-token",
			RefreshToken: "rt-new-token",
			ExpiresIn:    3600,
		},
	}
	svc := NewOpenAIOAuthService(nil, oauthClient)
	defer svc.Stop()

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "rt-token",
			"client_id":     "client-id-1",
		},
	}

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "rt-access-token", info.AccessToken)
	require.Equal(t, "rt-token", oauthClient.lastRefreshToken)
	require.Equal(t, "client-id-1", oauthClient.lastClientID)
}

func TestOpenAIOAuthService_RefreshAccountToken_NoRecoveryCredential(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no session_token, refresh_token, or access_token available")
}

type openaiOAuthClientNoopStub struct{}

func (s *openaiOAuthClientNoopStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientNoopStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientNoopStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

type openaiOAuthClientTrackingStub struct {
	tokenResponse    *openai.TokenResponse
	lastRefreshToken string
	lastProxyURL     string
	lastClientID     string
}

func (s *openaiOAuthClientTrackingStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientTrackingStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientTrackingStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	s.lastRefreshToken = refreshToken
	s.lastProxyURL = proxyURL
	s.lastClientID = clientID
	return s.tokenResponse, nil
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=st-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at-token","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"}}`))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	info, err := svc.ExchangeSoraSessionToken(context.Background(), "st-token", nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "at-token", info.AccessToken)
	require.Equal(t, "demo@example.com", info.Email)
	require.Greater(t, info.ExpiresAt, int64(0))
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_UsesAccessTokenExp(t *testing.T) {
	exp := time.Now().Add(15 * time.Minute).Unix()
	accessToken := makeUnsignedJWT(fmt.Sprintf(`{"exp":%d}`, exp))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=st-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"accessToken":"%s","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"}}`, accessToken)))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	info, err := svc.ExchangeSoraSessionToken(context.Background(), "st-token", nil)
	require.NoError(t, err)
	require.Equal(t, exp, info.ExpiresAt)
	require.Equal(t, "st-token", info.SessionToken)
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"expires":"2099-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	_, err := svc.ExchangeSoraSessionToken(context.Background(), "st-token", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing access token")
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_AcceptsSetCookieLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=st-cookie-value")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at-token","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"}}`))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	raw := "__Secure-next-auth.session-token.0=st-cookie-value; Domain=.chatgpt.com; Path=/; HttpOnly; Secure; SameSite=Lax"
	info, err := svc.ExchangeSoraSessionToken(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Equal(t, "at-token", info.AccessToken)
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_MergesChunkedSetCookieLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=chunk-0chunk-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at-token","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"}}`))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	raw := strings.Join([]string{
		"Set-Cookie: __Secure-next-auth.session-token.1=chunk-1; Path=/; HttpOnly",
		"Set-Cookie: __Secure-next-auth.session-token.0=chunk-0; Path=/; HttpOnly",
	}, "\n")
	info, err := svc.ExchangeSoraSessionToken(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Equal(t, "at-token", info.AccessToken)
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_PrefersLatestDuplicateChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=new-0new-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at-token","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"}}`))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	raw := strings.Join([]string{
		"Set-Cookie: __Secure-next-auth.session-token.0=old-0; Path=/; HttpOnly",
		"Set-Cookie: __Secure-next-auth.session-token.1=old-1; Path=/; HttpOnly",
		"Set-Cookie: __Secure-next-auth.session-token.0=new-0; Path=/; HttpOnly",
		"Set-Cookie: __Secure-next-auth.session-token.1=new-1; Path=/; HttpOnly",
	}, "\n")
	info, err := svc.ExchangeSoraSessionToken(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Equal(t, "at-token", info.AccessToken)
}

func TestOpenAIOAuthService_ExchangeSoraSessionToken_UsesLatestCompleteChunkGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.Header.Get("Cookie"), "__Secure-next-auth.session-token=ok-0ok-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"at-token","expires":"2099-01-01T00:00:00Z","user":{"email":"demo@example.com"}}`))
	}))
	defer server.Close()

	origin := openAISoraSessionAuthURL
	openAISoraSessionAuthURL = server.URL
	defer func() { openAISoraSessionAuthURL = origin }()

	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientNoopStub{})
	defer svc.Stop()

	raw := strings.Join([]string{
		"set-cookie",
		"__Secure-next-auth.session-token.0=ok-0; Domain=.chatgpt.com; Path=/",
		"set-cookie",
		"__Secure-next-auth.session-token.1=ok-1; Domain=.chatgpt.com; Path=/",
		"set-cookie",
		"__Secure-next-auth.session-token.0=partial-0; Domain=.chatgpt.com; Path=/",
	}, "\n")
	info, err := svc.ExchangeSoraSessionToken(context.Background(), raw, nil)
	require.NoError(t, err)
	require.Equal(t, "at-token", info.AccessToken)
}
