package buildaccount

import (
	"context"

	"github.com/dslzl/gork/app/control/account"
)

// AutoCleanAdapter 将 Store 适配为 account.BuildAccountAutoCleanStore。
type AutoCleanAdapter struct {
	Store Store
}

// List 返回未软删账号的 status/updated 视图。
func (a AutoCleanAdapter) List(ctx context.Context) ([]account.BuildAutoCleanAccount, error) {
	if a.Store == nil {
		return nil, nil
	}
	items, err := a.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]account.BuildAutoCleanAccount, 0, len(items))
	for _, item := range items {
		out = append(out, account.BuildAutoCleanAccount{
			ID:        item.ID,
			Status:    item.Status,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return out, nil
}

// Delete 软删。
func (a AutoCleanAdapter) Delete(ctx context.Context, id int64) error {
	if a.Store == nil {
		return nil
	}
	return a.Store.Delete(ctx, id)
}
