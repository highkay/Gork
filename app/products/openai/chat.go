package openai

import (
	"context"
	"fmt"

	controlmodel "github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/products"
)

type chatRuntimeDependencies struct {
	directory          func() chatDirectory
	consoleCompletions func(context.Context, chatCompletionOptions) (chatCompletionResult, error)
}

func defaultChatRuntimeDependencies() chatRuntimeDependencies {
	return chatRuntimeDependencies{
		directory:          chatDirectoryProvider,
		consoleCompletions: consoleCompletions,
	}
}

func Completions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
	return defaultChatRuntimeDependencies().Completions(ctx, options)
}

func (deps chatRuntimeDependencies) Completions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
	plan, err := prepareChatCompletion(options)
	if err != nil {
		return chatCompletionResult{}, err
	}
	if plan.IsConsole {
		stream := plan.IsStream
		emitThink := plan.EmitThink
		options.Stream = &stream
		options.EmitThink = &emitThink
		if deps.consoleCompletions == nil {
			return chatCompletionResult{}, fmt.Errorf("console chat completions are not configured")
		}
		return deps.consoleCompletions(ctx, options)
	}

	if deps.directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}
	directory := deps.directory()
	if directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}

	dispatchDirectory := newChatDispatchDirectory(directory)
	return products.RunAccountDispatch(ctx, products.AccountDispatchOptions[chatCompletionResult]{
		Directory:         dispatchDirectory,
		Spec:              plan.Spec,
		Retry:             products.RetryPolicy{MaxAttempts: plan.MaxRetries + 1},
		Retryable:         func(err error) bool { return shouldRetryUpstream(err, plan.RetryCodes) },
		Feedback:          chatDispatchFeedback,
		NoAccountsMessage: "No available accounts for this model tier",
	}, func(ctx context.Context, lease products.AccountDispatchLease) (chatCompletionResult, error) {
		account, ok := dispatchDirectory.account(lease)
		if !ok {
			return chatCompletionResult{}, fmt.Errorf("missing chat dispatch account for %s", lease.Token)
		}
		result, err := runChatCompletionAttempt(ctx, options, plan, account)
		if err == nil {
			quotaSync(ctx, account.Token, int(account.ModeID))
		} else {
			failSync(ctx, account.Token, int(account.ModeID), err)
		}
		return result, err
	})
}

type chatDispatchDirectory struct {
	directory chatDirectory
	accounts  map[products.AccountDispatchLease]chatAccount
}

func newChatDispatchDirectory(directory chatDirectory) *chatDispatchDirectory {
	return &chatDispatchDirectory{
		directory: directory,
		accounts:  map[products.AccountDispatchLease]chatAccount{},
	}
}

func (d *chatDispatchDirectory) ReserveDispatchAccount(ctx context.Context, query products.AccountDispatchQuery) (products.AccountDispatchLease, bool, error) {
	account, ok, err := d.directory.ReserveChatAccount(ctx, query.Spec, query.Excluded)
	if err != nil || !ok {
		return products.AccountDispatchLease{}, ok, err
	}
	lease := products.AccountDispatchLease{Token: account.Token, ModeID: int(account.ModeID)}
	d.accounts[lease] = account
	return lease, true, nil
}

func (d *chatDispatchDirectory) ReleaseDispatchAccount(ctx context.Context, lease products.AccountDispatchLease) error {
	account, ok := d.accounts[lease]
	if !ok {
		return nil
	}
	delete(d.accounts, lease)
	return d.directory.ReleaseChatAccount(ctx, account)
}

func (d *chatDispatchDirectory) FeedbackDispatchAccount(ctx context.Context, feedback products.AccountDispatchFeedback) error {
	return d.directory.FeedbackChatAccount(ctx, chatFeedback{
		Token:  feedback.Token,
		Kind:   accountFeedbackKind(feedback.Kind),
		ModeID: controlmodel.ModeID(feedback.ModeID),
	})
}

func (d *chatDispatchDirectory) account(lease products.AccountDispatchLease) (chatAccount, bool) {
	account, ok := d.accounts[lease]
	return account, ok
}

func chatDispatchFeedback(err error) string {
	if err == nil {
		return string(feedbackKindSuccess)
	}
	return string(feedbackKind(err))
}

func runChatCompletionAttempt(ctx context.Context, options chatCompletionOptions, plan chatCompletionPlan, account chatAccount) (chatCompletionResult, error) {
	lines, err := streamChat(ctx, chatStreamOptions{
		Token:               account.Token,
		ModeID:              account.ModeID,
		Message:             plan.Message,
		Files:               plan.Files,
		ToolOverrides:       plan.ToolOverrides,
		ModelConfigOverride: nil,
		RequestOverrides:    plan.RequestOverrides,
		TimeoutSeconds:      plan.TimeoutSeconds,
	})
	if err != nil {
		return chatCompletionResult{}, err
	}

	state, frames, err := consumeChatLines(lines, consumeChatLinesOptions{
		Context:      ctx,
		Token:        account.Token,
		Model:        options.Model,
		ResponseID:   plan.ResponseID,
		EmitThink:    plan.EmitThink,
		IsStream:     plan.IsStream,
		ToolNames:    plan.ToolNames,
		DisableTools: plan.ToolsDisabled,
	})
	if err != nil {
		return chatCompletionResult{}, err
	}

	if plan.IsStream {
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}

	response, err := buildNonStreamChatResponse(chatResponseBuildOptions{
		Model:      options.Model,
		Message:    plan.Message,
		ResponseID: plan.ResponseID,
		ToolNames:  plan.ToolNames,
		EmitThink:  plan.EmitThink,
		State:      state,
	})
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{Response: response}, nil
}

func finishChatAttempt(ctx context.Context, directory chatDirectory, account chatAccount, success bool, err error) {
	_ = directory.ReleaseChatAccount(ctx, account)
	kind := feedbackKindSuccess
	if !success {
		kind = feedbackKind(err)
	}
	_ = directory.FeedbackChatAccount(ctx, chatFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
	if success {
		quotaSync(ctx, account.Token, int(account.ModeID))
	} else if err != nil {
		failSync(ctx, account.Token, int(account.ModeID), err)
	}
}
