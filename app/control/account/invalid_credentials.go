package account

import (
	"context"
	"errors"
	"time"

	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/logging"
)

const (
	invalidCredentialsReason = "invalid_credentials"
	invalidCredentialsExtKey = "invalid_credentials"
)

var (
	invalidCredentialsNowMS = func() int64 {
		return time.Now().UnixMilli()
	}
	invalidCredentialsMaxFailures = func() int {
		return platformconfig.GlobalConfig.GetInt("account.invalid_credentials.max_failures", 3)
	}
)

type InvalidCredentialRepository interface {
	GetAccounts(context.Context, []string) ([]AccountRecord, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error)
}

type invalidCredentialDeleteRepository interface {
	DeleteAccounts(context.Context, []string) (AccountMutationResult, error)
}

func MarkAccountInvalidCredentials(
	ctx context.Context,
	repo InvalidCredentialRepository,
	token string,
	err error,
	source string,
) (bool, error) {
	if !protocol.IsInvalidCredentialsError(err) {
		return false, nil
	}
	records, getErr := repo.GetAccounts(ctx, []string{token})
	if getErr != nil {
		return false, getErr
	}
	ts := invalidCredentialsNowMS()
	reason := invalidCredentialsReason
	failureCount := invalidCredentialsFailureCount(records) + 1
	patch := AccountPatch{
		Token:          token,
		LastFailAt:     &ts,
		LastFailReason: &reason,
		StateReason:    &reason,
		ExtMerge:       invalidCredentialsExt(records, ts, reason, failureCount, source, err),
	}
	if failureCount >= invalidCredentialsFailureThreshold() {
		status := AccountStatusExpired
		patch.Status = &status
		patch.ExtMerge[expiredAtKey] = ts
		patch.ExtMerge[expiredReasonKey] = reason
	}
	if _, patchErr := repo.PatchAccounts(ctx, []AccountPatch{patch}); patchErr != nil {
		return false, patchErr
	}
	if patch.Status != nil && *patch.Status == AccountStatusExpired {
		if deleteRepo, ok := repo.(invalidCredentialDeleteRepository); ok {
			if _, deleteErr := deleteRepo.DeleteAccounts(ctx, []string{token}); deleteErr != nil {
				return false, deleteErr
			}
			logInvalidCredentialsDeleted(source, token, err, failureCount)
		} else {
			logInvalidCredentialsMarked(source, token, err, failureCount)
		}
	}
	return true, nil
}

func FeedbackKindForError(err error) FeedbackKind {
	if err == nil {
		return FeedbackKindServerError
	}
	if protocol.IsInvalidCredentialsError(err) {
		return FeedbackKindUnauthorized
	}
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		return FeedbackKindServerError
	}
	switch {
	case upstream.Status == 429:
		return FeedbackKindRateLimited
	case upstream.Status == 401:
		return FeedbackKindUnauthorized
	case upstream.Status == 403:
		return FeedbackKindForbidden
	default:
		return FeedbackKindServerError
	}
}

func invalidCredentialsExt(records []AccountRecord, ts int64, reason string, failureCount int, source string, err error) map[string]any {
	ext := map[string]any{}
	if len(records) > 0 {
		ext = cloneAnyMap(records[0].Ext)
	}
	ext[invalidCredentialsExtKey] = map[string]any{
		"failure_count":    failureCount,
		"last_fail_at":     ts,
		"last_fail_reason": reason,
		"last_fail_source": source,
		"last_fail_error":  err.Error(),
	}
	return ext
}

func invalidCredentialsFailureThreshold() int {
	value := invalidCredentialsMaxFailures()
	if value <= 0 {
		return 3
	}
	return value
}

func invalidCredentialsFailureCount(records []AccountRecord) int {
	if len(records) == 0 {
		return 0
	}
	raw, ok := records[0].Ext[invalidCredentialsExtKey]
	if !ok {
		return 0
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return 0
	}
	count, err := intFromAny(data["failure_count"], 0)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func logInvalidCredentialsMarked(source string, token string, err error, failureCount int) {
	logInvalidCredentialsStateChange("account expired from invalid credentials", source, token, err, failureCount, string(AccountStatusExpired))
}

func logInvalidCredentialsDeleted(source string, token string, err error, failureCount int) {
	logInvalidCredentialsStateChange("account deleted from invalid credentials", source, token, err, failureCount, "deleted")
}

func logInvalidCredentialsStateChange(message string, source string, token string, err error, failureCount int, status string) {
	var upstream *platform.UpstreamError
	upstreamStatus := 0
	if errors.As(err, &upstream) {
		upstreamStatus = upstream.Status
	}
	logging.Logger.Info(
		message,
		"source", source,
		"token", tokenPrefix(token),
		"status", status,
		"failure_count", failureCount,
		"upstream_status", upstreamStatus,
	)
}

func tokenPrefix(token string) string {
	if len(token) <= 10 {
		return token
	}
	return token[:10]
}
