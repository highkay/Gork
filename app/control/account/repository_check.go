package account

import (
	"context"
	"fmt"
)

type AccountConsistencyIssue struct {
	Code    string `json:"code"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message"`
}

type AccountConsistencyReport struct {
	OK               bool                      `json:"ok"`
	Revision         int                       `json:"revision"`
	SnapshotRevision int                       `json:"snapshot_revision"`
	ListRevision     int                       `json:"list_revision"`
	SnapshotCount    int                       `json:"snapshot_count"`
	ListCount        int                       `json:"list_count"`
	Issues           []AccountConsistencyIssue `json:"issues"`
}

func CheckAccountRepositoryConsistency(ctx context.Context, repo AccountRepository) (AccountConsistencyReport, error) {
	report := AccountConsistencyReport{OK: true, Issues: []AccountConsistencyIssue{}}
	revision, err := repo.GetRevision(ctx)
	if err != nil {
		return report, err
	}
	snapshot, err := repo.RuntimeSnapshot(ctx)
	if err != nil {
		return report, err
	}
	items, listRevision, err := listAllAccountsForCheck(ctx, repo)
	if err != nil {
		return report, err
	}
	report.Revision = revision
	report.SnapshotRevision = snapshot.Revision
	report.ListRevision = listRevision
	report.SnapshotCount = len(snapshot.Items)
	report.ListCount = len(items)
	report.addRevisionIssue("snapshot_revision_mismatch", snapshot.Revision, revision)
	if listRevision != 0 {
		report.addRevisionIssue("list_revision_mismatch", listRevision, revision)
	}
	report.checkRecordSet("snapshot", snapshot.Items)
	report.checkRecordSet("list", items)
	report.checkIndexConsistency(ctx, repo, snapshot.Items, items)
	report.OK = len(report.Issues) == 0
	return report, nil
}

func listAllAccountsForCheck(ctx context.Context, repo AccountRepository) ([]AccountRecord, int, error) {
	out := []AccountRecord{}
	revision := 0
	for page := 1; ; page++ {
		result, err := repo.ListAccounts(ctx, ListAccountsQuery{Page: page, PageSize: 2000, SortBy: "token"})
		if err != nil {
			return nil, 0, err
		}
		if revision == 0 {
			revision = result.Revision
		}
		out = append(out, result.Items...)
		if len(result.Items) == 0 || page >= result.TotalPages {
			break
		}
	}
	return out, revision, nil
}

func (r *AccountConsistencyReport) addIssue(code string, token string, message string) {
	r.Issues = append(r.Issues, AccountConsistencyIssue{Code: code, Token: token, Message: message})
}

func (r *AccountConsistencyReport) addRevisionIssue(code string, got int, want int) {
	if got != want {
		r.addIssue(code, "", fmt.Sprintf("revision %d does not match repository revision %d", got, want))
	}
}

func (r *AccountConsistencyReport) checkRecordSet(source string, records []AccountRecord) {
	seen := map[string]bool{}
	for _, record := range records {
		if record.Token == "" {
			r.addIssue(source+"_empty_token", "", "record token is empty")
			continue
		}
		if seen[record.Token] {
			r.addIssue(source+"_duplicate_token", record.Token, "token appears more than once")
		}
		seen[record.Token] = true
		r.checkRecordFields(source, record)
	}
}

func (r *AccountConsistencyReport) checkRecordFields(source string, record AccountRecord) {
	if record.IsDeleted() {
		r.addIssue(source+"_deleted_visible", record.Token, "deleted account is visible in active account set")
	}
	if _, ok := ParseAccountStatus(record.Status.String()); !ok {
		r.addIssue(source+"_invalid_status", record.Token, fmt.Sprintf("unknown status %q", record.Status))
	}
	if _, err := NormalizeAccountPool(record.Pool); err != nil {
		r.addIssue(source+"_invalid_pool", record.Token, err.Error())
	}
	if normalized := NormalizeAccountTags(record.Tags); len(normalized) != len(record.Tags) {
		r.addIssue(source+"_invalid_tags", record.Token, "tags are not normalized")
	}
	if record.Ext == nil {
		r.addIssue(source+"_nil_ext", record.Token, "ext is nil")
	}
	if _, err := record.QuotaSet(); err != nil {
		r.addIssue(source+"_invalid_quota", record.Token, err.Error())
	}
}

func (r *AccountConsistencyReport) checkIndexConsistency(ctx context.Context, repo AccountRepository, snapshot []AccountRecord, listed []AccountRecord) {
	snapshotTokens := tokenSet(snapshot)
	listTokens := tokenSet(listed)
	for token := range snapshotTokens {
		if !listTokens[token] {
			r.addIssue("list_missing_snapshot_token", token, "token exists in RuntimeSnapshot but not in ListAccounts")
		}
	}
	for token := range listTokens {
		if !snapshotTokens[token] {
			r.addIssue("snapshot_missing_list_token", token, "token exists in ListAccounts but not in RuntimeSnapshot")
		}
	}
	tokens := make([]string, 0, len(snapshotTokens))
	for token := range snapshotTokens {
		tokens = append(tokens, token)
	}
	for start := 0; start < len(tokens); start += 500 {
		end := start + 500
		if end > len(tokens) {
			end = len(tokens)
		}
		records, err := repo.GetAccounts(ctx, tokens[start:end])
		if err != nil {
			r.addIssue("get_accounts_error", "", err.Error())
			return
		}
		got := tokenSet(records)
		for _, token := range tokens[start:end] {
			if !got[token] {
				r.addIssue("get_accounts_missing_token", token, "token exists in RuntimeSnapshot but not in GetAccounts")
			}
		}
	}
}

func tokenSet(records []AccountRecord) map[string]bool {
	out := map[string]bool{}
	for _, record := range records {
		if record.Token != "" {
			out[record.Token] = true
		}
	}
	return out
}
