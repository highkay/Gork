package anthropic

import (
	"context"
	"errors"
	"strings"

	controlaccount "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/products"
)

func Messages(ctx context.Context, options MessagesOptions) (MessagesResult, error) {
	plan, err := prepareMessages(options)
	if err != nil {
		return MessagesResult{}, err
	}
	if plan.Spec.IsConsoleChat() {
		return messagesConsole(ctx, options, plan)
	}
	directory := messagesDirectoryProvider()
	if directory == nil {
		return MessagesResult{}, platform.NewRateLimitError("Account directory not initialised")
	}
	return runMessagesWithRetries(ctx, options, plan, directory)
}

func prepareMessages(options MessagesOptions) (messagesPlan, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return messagesPlan{}, err
	}
	internal := parseAnthropicMessages(options.Messages, options.System)
	message, files := extractAnthropicMessage(internal)
	if strings.TrimSpace(message) == "" {
		return messagesPlan{}, platform.NewUpstreamError("Empty message after extraction", 400, "")
	}
	return buildMessagesPlan(options, spec, internal, message, files), nil
}

func buildMessagesPlan(options MessagesOptions, spec model.ModelSpec, internal []map[string]any, message string, files []string) messagesPlan {
	toolNames := []string{}
	if len(options.Tools) > 0 {
		chatTools := convertAnthropicTools(options.Tools)
		toolNames = protocol.ExtractToolNames(chatTools)
		toolPrompt := protocol.BuildToolSystemPrompt(chatTools, convertAnthropicToolChoice(options.ToolChoice))
		message = protocol.InjectIntoMessage(message, toolPrompt)
	}
	messageID := options.MessageID
	if messageID == "" {
		messageID = makeAnthropicMessageID()
	}
	return messagesPlan{Spec: spec, IsStream: options.Stream, EmitThink: options.EmitThink, Internal: internal,
		Message: message, Files: files, ToolNames: toolNames, MessageID: messageID,
		MaxRetries: messagesMaxRetries(), RetryCodes: messagesRetryCodes(), TimeoutSeconds: messagesTimeoutSeconds()}
}

func messagesConsole(ctx context.Context, options MessagesOptions, plan messagesPlan) (MessagesResult, error) {
	result, err := ConsoleMessages(ctx, ConsoleMessagesOptions{
		Model: options.Model, Messages: plan.Internal, Stream: options.Stream, EmitThink: options.EmitThink,
		Temperature: options.Temperature, TopP: options.TopP, MessageID: plan.MessageID,
	})
	if err != nil {
		return MessagesResult{}, err
	}
	return MessagesResult(result), nil
}

func runMessagesWithRetries(ctx context.Context, options MessagesOptions, plan messagesPlan, directory messagesDirectory) (MessagesResult, error) {
	dispatchDirectory := newMessagesDispatchDirectory(directory)
	return products.RunAccountDispatch(ctx, products.AccountDispatchOptions[MessagesResult]{
		Directory:         dispatchDirectory,
		Spec:              plan.Spec,
		Retry:             products.RetryPolicy{MaxAttempts: plan.MaxRetries + 1},
		Retryable:         func(err error) bool { return shouldRetryMessages(err, plan.RetryCodes) },
		Feedback:          messagesDispatchFeedback,
		NoAccountsMessage: "No available accounts for this model tier",
	}, func(ctx context.Context, lease products.AccountDispatchLease) (MessagesResult, error) {
		account, ok := dispatchDirectory.account(lease)
		if !ok {
			return MessagesResult{}, platform.NewRateLimitError("No available accounts for this model tier")
		}
		return runMessagesAttempt(ctx, options, plan, account)
	})
}

type messagesDispatchDirectory struct {
	directory messagesDirectory
	accounts  map[products.AccountDispatchLease]messagesAccount
}

func newMessagesDispatchDirectory(directory messagesDirectory) *messagesDispatchDirectory {
	return &messagesDispatchDirectory{
		directory: directory,
		accounts:  map[products.AccountDispatchLease]messagesAccount{},
	}
}

func (d *messagesDispatchDirectory) ReserveDispatchAccount(ctx context.Context, query products.AccountDispatchQuery) (products.AccountDispatchLease, bool, error) {
	account, ok, err := d.directory.ReserveMessagesAccount(ctx, query.Spec, query.Excluded)
	if err != nil || !ok {
		return products.AccountDispatchLease{}, ok, err
	}
	lease := products.AccountDispatchLease{Token: account.Token, ModeID: int(account.ModeID)}
	d.accounts[lease] = account
	return lease, true, nil
}

func (d *messagesDispatchDirectory) ReleaseDispatchAccount(ctx context.Context, lease products.AccountDispatchLease) error {
	account, ok := d.accounts[lease]
	if !ok {
		return nil
	}
	delete(d.accounts, lease)
	return d.directory.ReleaseMessagesAccount(ctx, account)
}

func (d *messagesDispatchDirectory) FeedbackDispatchAccount(ctx context.Context, feedback products.AccountDispatchFeedback) error {
	return d.directory.FeedbackMessagesAccount(ctx, messagesFeedback{
		Token:  feedback.Token,
		Kind:   messagesFeedbackKind(feedback.Kind),
		ModeID: model.ModeID(feedback.ModeID),
	})
}

func (d *messagesDispatchDirectory) account(lease products.AccountDispatchLease) (messagesAccount, bool) {
	account, ok := d.accounts[lease]
	return account, ok
}

func messagesDispatchFeedback(err error) string {
	if err == nil {
		return string(messagesFeedbackSuccess)
	}
	return string(messagesFeedbackForError(err))
}

func runMessagesAttempt(ctx context.Context, options MessagesOptions, plan messagesPlan, account messagesAccount) (MessagesResult, error) {
	result, err := messagesFromStream(ctx, options, plan, account)
	if err != nil {
		messagesFailSync(ctx, account.Token, int(account.ModeID), err)
		return MessagesResult{}, err
	}
	messagesQuotaSync(ctx, account.Token, int(account.ModeID))
	return result, nil
}

func shouldRetryMessages(err error, retryCodes map[int]struct{}) bool {
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream == nil {
		return false
	}
	_, ok := retryCodes[upstream.Status]
	return ok
}

func messagesFeedbackForError(err error) messagesFeedbackKind {
	return messagesFeedbackKind(controlaccount.FeedbackKindForError(err))
}
