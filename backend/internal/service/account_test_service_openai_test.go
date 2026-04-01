//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIAccountTestRepo struct {
	mockAccountRepoForGemini
	updatedExtra  map[string]any
	rateLimitedID int64
	rateLimitedAt *time.Time
	tempCalls     int
	tempUntil     *time.Time
	tempReason    string
	deleteCalls   int
	deletedIDs    []int64
	setErrorCalls int
	lastErrorMsg  string
}

func (r *openAIAccountTestRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updatedExtra = updates
	return nil
}

func (r *openAIAccountTestRepo) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitedID = id
	r.rateLimitedAt = &resetAt
	return nil
}

func (r *openAIAccountTestRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.tempCalls++
	r.tempUntil = &until
	r.tempReason = reason
	return nil
}

func (r *openAIAccountTestRepo) Delete(_ context.Context, id int64) error {
	r.deleteCalls++
	r.deletedIDs = append(r.deletedIDs, id)
	return nil
}

func (r *openAIAccountTestRepo) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

func TestAccountTestService_OpenAISuccessPersistsSnapshotFromHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, recorder := newSoraTestContext()

	resp := newJSONResponse(http.StatusOK, "")
	resp.Body = io.NopCloser(strings.NewReader(`data: {"type":"response.completed"}

`))
	resp.Header.Set("x-codex-primary-used-percent", "88")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "604800")
	resp.Header.Set("x-codex-primary-window-minutes", "10080")
	resp.Header.Set("x-codex-secondary-used-percent", "42")
	resp.Header.Set("x-codex-secondary-reset-after-seconds", "18000")
	resp.Header.Set("x-codex-secondary-window-minutes", "300")

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:          89,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4")
	require.NoError(t, err)
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, 42.0, repo.updatedExtra["codex_5h_used_percent"])
	require.Equal(t, 88.0, repo.updatedExtra["codex_7d_used_percent"])
	require.Contains(t, recorder.Body.String(), "test_complete")
}

func TestAccountTestService_OpenAI429PersistsSnapshotAndRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newSoraTestContext()

	resp := newJSONResponse(http.StatusTooManyRequests, `{"error":{"type":"usage_limit_reached","message":"limit reached"}}`)
	resp.Header.Set("x-codex-primary-used-percent", "100")
	resp.Header.Set("x-codex-primary-reset-after-seconds", "604800")
	resp.Header.Set("x-codex-primary-window-minutes", "10080")
	resp.Header.Set("x-codex-secondary-used-percent", "100")
	resp.Header.Set("x-codex-secondary-reset-after-seconds", "18000")
	resp.Header.Set("x-codex-secondary-window-minutes", "300")

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	account := &Account{
		ID:          88,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4")
	require.Error(t, err)
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, 100.0, repo.updatedExtra["codex_5h_used_percent"])
	require.Equal(t, int64(88), repo.rateLimitedID)
	require.NotNil(t, repo.rateLimitedAt)
	require.NotNil(t, account.RateLimitResetAt)
	if account.RateLimitResetAt != nil && repo.rateLimitedAt != nil {
		require.WithinDuration(t, *repo.rateLimitedAt, *account.RateLimitResetAt, time.Second)
	}
}

func TestAccountTestService_OpenAI401NoOrganizationSetsTempUnschedulable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newSoraTestContext()

	resp := newJSONResponse(http.StatusUnauthorized, `{"error":{"message":"You must be a member of an organization to use the API. Please contact us through our help center at help.openai.com.","type":"invalid_request_error","code":"no_organization","param":null},"status":401}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{},
	}
	account := &Account{
		ID:          90,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4")
	require.Error(t, err)
	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, repo.tempUntil)
	require.Contains(t, repo.tempReason, "OpenAI organization membership pending")
	if repo.tempUntil != nil {
		require.WithinDuration(t, time.Now().Add(10*time.Minute), *repo.tempUntil, 3*time.Second)
	}
}

func TestAccountTestService_OpenAI401TokenRevokedDeletesAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newSoraTestContext()

	resp := newJSONResponse(http.StatusUnauthorized, `{"error":{"message":"Encountered invalidated oauth token for user, failing request","code":"token_revoked"},"status":401}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	rateLimitSvc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc := &AccountTestService{
		accountRepo:      repo,
		httpUpstream:     upstream,
		cfg:              &config.Config{},
		rateLimitService: rateLimitSvc,
	}
	account := &Account{
		ID:          91,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4")
	require.Error(t, err)
	require.Equal(t, 1, repo.deleteCalls)
	require.Equal(t, []int64{91}, repo.deletedIDs)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
}

func TestAccountTestService_OpenAI401TokenInvalidatedDeletesAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := newSoraTestContext()

	resp := newJSONResponse(http.StatusUnauthorized, `{"error":{"message":"Your authentication token has been invalidated. Please try signing in again.","type":"invalid_request_error","code":"token_invalidated","param":null},"status":401}`)

	repo := &openAIAccountTestRepo{}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
	rateLimitSvc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc := &AccountTestService{
		accountRepo:      repo,
		httpUpstream:     upstream,
		cfg:              &config.Config{},
		rateLimitService: rateLimitSvc,
	}
	account := &Account{
		ID:          92,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-token", "session_token": "st-token"},
	}

	err := svc.testOpenAIAccountConnection(ctx, account, "gpt-5.4")
	require.Error(t, err)
	require.Equal(t, 1, repo.deleteCalls)
	require.Equal(t, []int64{92}, repo.deletedIDs)
	require.Equal(t, 0, repo.tempCalls)
	require.Equal(t, 0, repo.setErrorCalls)
}
