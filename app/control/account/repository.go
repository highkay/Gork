package account

import "context"

const AccountScanChangesDefaultLimit = 5000

type AccountRepository interface {
	Initialize(context.Context) error
	GetRevision(context.Context) (int, error)
	RuntimeSnapshot(context.Context) (RuntimeSnapshot, error)
	// ScanChanges returns the complete next revision group after sinceRevision.
	// Revision numbers are monotonic cursors and may be sparse.
	// limit may cap revision selection work, but implementations must not split
	// records that share the selected revision.
	ScanChanges(context.Context, int, int) (AccountChangeSet, error)
	UpsertAccounts(context.Context, []AccountUpsert) (AccountMutationResult, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error)
	DeleteAccounts(context.Context, []string) (AccountMutationResult, error)
	GetAccounts(context.Context, []string) ([]AccountRecord, error)
	ListAccounts(context.Context, ListAccountsQuery) (AccountPage, error)
	// ReplacePool replaces a pool as a delete revision followed by an upsert
	// revision; returned Revision is the final upsert revision.
	ReplacePool(context.Context, BulkReplacePoolCommand) (AccountMutationResult, error)
	Close(context.Context) error
}
