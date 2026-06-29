package app

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
	"github.com/dslzl/gork/app/platform/config"
	configbackends "github.com/dslzl/gork/app/platform/config/backends"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
	platformstartup "github.com/dslzl/gork/app/platform/startup"
)

func TestDefaultStartupMigrationsUseConfigBackendAndRepositoryAdapter(t *testing.T) {
	repo := &lifecycleAccountRepository{
		snapshot: accountcontrol.RuntimeSnapshot{
			Revision: 7,
			Items: []accountcontrol.AccountRecord{{
				Token:         "tok-1",
				Pool:          "basic",
				Status:        accountcontrol.AccountStatusActive,
				Tags:          []string{"alpha"},
				Quota:         map[string]any{"auto": map[string]any{"remaining": 1}},
				UsageUseCount: 2,
				Ext:           map[string]any{"k": "v"},
			}},
		},
	}
	state := &appMainLifecycleState{repository: repo}
	configBackend := lifecycleConfigBackend{}
	migratorCalled := false
	configReloaded := false
	oldCreateConfigBackend := appMainCreateConfigBackend
	oldStartupMigrator := appMainStartupMigrator
	oldLoadRequestConfig := appMainLoadRequestConfig
	t.Cleanup(func() {
		appMainCreateConfigBackend = oldCreateConfigBackend
		appMainStartupMigrator = oldStartupMigrator
		appMainLoadRequestConfig = oldLoadRequestConfig
	})
	appMainCreateConfigBackend = func(configbackends.FactoryOptions) (configbackends.ConfigBackend, error) {
		return configBackend, nil
	}
	appMainLoadRequestConfig = func(context.Context) error {
		configReloaded = true
		return nil
	}
	appMainStartupMigrator = func(ctx context.Context, config platformstartup.ConfigBackend, migrationRepo platformstartup.AccountRepository, _ platformstartup.StartupMigrationOptions) error {
		migratorCalled = true
		if config != configBackend {
			t.Fatalf("config backend was not passed through")
		}
		snapshot, err := migrationRepo.RuntimeSnapshot(ctx)
		if err != nil {
			t.Fatalf("snapshot error: %v", err)
		}
		if snapshot.Revision != 7 || len(snapshot.Items) != 1 || snapshot.Items[0].Token != "tok-1" {
			t.Fatalf("snapshot=%#v", snapshot)
		}
		page, err := migrationRepo.ListAccounts(ctx, platformstartup.ListAccountsQuery{
			Page:           2,
			PageSize:       3,
			Pool:           "basic",
			IncludeDeleted: true,
		})
		if err != nil {
			t.Fatalf("list error: %v", err)
		}
		if page.TotalPages != 1 || len(page.Items) != 1 || repo.listQuery.Page != 2 || repo.listQuery.Pool == nil || *repo.listQuery.Pool != "basic" {
			t.Fatalf("page=%#v listQuery=%#v", page, repo.listQuery)
		}
		one := 1
		if _, err := migrationRepo.UpsertAccounts(ctx, []platformstartup.AccountUpsert{{
			Token: "tok-2",
			Pool:  "super",
			Tags:  []string{"beta"},
			Ext:   map[string]any{"source": "migration"},
		}}); err != nil {
			t.Fatalf("upsert error: %v", err)
		}
		if _, err := migrationRepo.PatchAccounts(ctx, []platformstartup.AccountPatch{{
			Token:          "tok-1",
			Status:         "disabled",
			QuotaAuto:      map[string]any{"remaining": 0},
			UsageUseDelta:  &one,
			LastUseAt:      int64(42),
			LastFailReason: "bad",
			ExtMerge:       map[string]any{"migrated": true},
		}}); err != nil {
			t.Fatalf("patch error: %v", err)
		}
		if _, err := migrationRepo.DeleteAccounts(ctx, []string{"old-token"}); err != nil {
			t.Fatalf("delete error: %v", err)
		}
		return nil
	}

	cleanup, err := defaultAppMainRunStartupMigrations(context.Background(), state)
	if err != nil {
		t.Fatalf("migration step error: %v", err)
	}
	if cleanup != nil {
		t.Fatalf("migration step should not register cleanup")
	}
	if !migratorCalled {
		t.Fatalf("startup migrator was not called")
	}
	if !configReloaded {
		t.Fatalf("config was not reloaded after startup migrations")
	}
	if len(repo.upserts) != 1 || repo.upserts[0].Token != "tok-2" || repo.upserts[0].Pool != "super" {
		t.Fatalf("upserts=%#v", repo.upserts)
	}
	if len(repo.patches) != 1 {
		t.Fatalf("patches=%#v", repo.patches)
	}
	patch := repo.patches[0]
	if patch.Status == nil || *patch.Status != accountcontrol.AccountStatusDisabled {
		t.Fatalf("patch status=%#v", patch.Status)
	}
	if patch.UsageUseDelta == nil || *patch.UsageUseDelta != 1 {
		t.Fatalf("usage delta=%#v", patch.UsageUseDelta)
	}
	if patch.LastUseAt == nil || *patch.LastUseAt != 42 {
		t.Fatalf("last use=%#v", patch.LastUseAt)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "bad" {
		t.Fatalf("last fail reason=%#v", patch.LastFailReason)
	}
	if !reflect.DeepEqual(repo.deleted, []string{"old-token"}) {
		t.Fatalf("deleted=%#v", repo.deleted)
	}
}

func TestRefreshRuntimeFallsBackToLocalSchedulerLockWithoutRedis(t *testing.T) {
	state := &appMainLifecycleState{repository: &lifecycleAccountRepository{}}
	lockAcquired := false
	lockReleased := false
	oldAcquireLocalLock := appMainAcquireSchedulerFileLock
	t.Cleanup(func() {
		appMainAcquireSchedulerFileLock = oldAcquireLocalLock
		accountcontrol.SetRefreshScheduler(nil)
		accountcontrol.SetRefreshSchedulerLeader(false)
		accountcontrol.SetRefreshService(nil)
	})
	appMainAcquireSchedulerFileLock = func(context.Context) (Hook, error) {
		lockAcquired = true
		return func(context.Context) error {
			lockReleased = true
			return nil
		}, nil
	}

	cleanup, err := defaultAppMainStartRefreshRuntime(context.Background(), state)
	if err != nil {
		t.Fatalf("refresh runtime error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("refresh runtime did not register cleanup")
	}
	if !lockAcquired {
		t.Fatalf("local scheduler lock was not acquired")
	}
	if !accountcontrol.IsRefreshSchedulerLeader() {
		t.Fatalf("local scheduler lock holder should be leader")
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if !lockReleased {
		t.Fatalf("local scheduler lock was not released")
	}
	if accountcontrol.IsRefreshSchedulerLeader() {
		t.Fatalf("scheduler leader flag was not cleared")
	}
}

func TestRefreshRuntimeUsesRedisLeaderLockWhenAvailable(t *testing.T) {
	redis := &lifecycleRuntimeRedis{locks: map[string]string{}}
	state := &appMainLifecycleState{
		repository:   &lifecycleAccountRepository{},
		runtimeStore: platformruntime.NewRedisRuntimeStore(redis, platformruntime.RedisRuntimeStoreOptions{}),
	}
	localLockCalled := false
	oldAcquireLocalLock := appMainAcquireSchedulerFileLock
	t.Cleanup(func() {
		appMainAcquireSchedulerFileLock = oldAcquireLocalLock
		accountcontrol.SetRefreshScheduler(nil)
		accountcontrol.SetSSOValidationScheduler(nil)
		accountcontrol.SetRefreshSchedulerLeader(false)
		accountcontrol.SetRefreshService(nil)
	})
	appMainAcquireSchedulerFileLock = func(context.Context) (Hook, error) {
		localLockCalled = true
		return nil, errors.New("local lock should not be used when redis lock is acquired")
	}

	cleanup, err := defaultAppMainStartRefreshRuntime(context.Background(), state)
	if err != nil {
		t.Fatalf("refresh runtime error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("refresh runtime did not register cleanup")
	}
	if localLockCalled {
		t.Fatalf("local scheduler lock should not be used when redis lock is acquired")
	}
	if state.schedulerKey == nil || !accountcontrol.IsRefreshSchedulerLeader() {
		t.Fatalf("redis lock holder should be scheduler leader")
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if redis.deletedKey != "runtime:lock:scheduler-leader" {
		t.Fatalf("redis lock release key = %q", redis.deletedKey)
	}
}

func TestRefreshRuntimeDoesNotFallbackWhenRedisLockIsHeld(t *testing.T) {
	redis := &lifecycleRuntimeRedis{locks: map[string]string{"runtime:lock:scheduler-leader": "other-worker"}}
	state := &appMainLifecycleState{
		repository:   &lifecycleAccountRepository{},
		runtimeStore: platformruntime.NewRedisRuntimeStore(redis, platformruntime.RedisRuntimeStoreOptions{}),
	}
	localLockCalled := false
	oldAcquireLocalLock := appMainAcquireSchedulerFileLock
	t.Cleanup(func() {
		appMainAcquireSchedulerFileLock = oldAcquireLocalLock
		accountcontrol.SetRefreshScheduler(nil)
		accountcontrol.SetSSOValidationScheduler(nil)
		accountcontrol.SetRefreshSchedulerLeader(false)
		accountcontrol.SetRefreshService(nil)
	})
	appMainAcquireSchedulerFileLock = func(context.Context) (Hook, error) {
		localLockCalled = true
		return nil, errors.New("local lock should not be used when redis lock is held")
	}

	cleanup, err := defaultAppMainStartRefreshRuntime(context.Background(), state)
	if err != nil {
		t.Fatalf("refresh runtime error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("refresh runtime did not register cleanup")
	}
	if localLockCalled {
		t.Fatalf("local scheduler lock should not be used when redis lock is held")
	}
	if state.schedulerKey != nil {
		t.Fatalf("redis follower scheduler key = %#v, set key=%q", state.schedulerKey, redis.setKey)
	}
	if accountcontrol.IsRefreshSchedulerLeader() {
		t.Fatalf("redis follower should not be scheduler leader, set key=%q existing locks=%#v", redis.setKey, redis.locks)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
}

func TestRefreshRuntimeFallsBackToLocalSchedulerLockWhenRedisUnavailable(t *testing.T) {
	redis := &lifecycleRuntimeRedis{setErr: errors.New("redis unavailable")}
	state := &appMainLifecycleState{
		repository:   &lifecycleAccountRepository{},
		runtimeStore: platformruntime.NewRedisRuntimeStore(redis, platformruntime.RedisRuntimeStoreOptions{}),
	}
	lockAcquired := false
	lockReleased := false
	oldAcquireLocalLock := appMainAcquireSchedulerFileLock
	t.Cleanup(func() {
		appMainAcquireSchedulerFileLock = oldAcquireLocalLock
		accountcontrol.SetRefreshScheduler(nil)
		accountcontrol.SetSSOValidationScheduler(nil)
		accountcontrol.SetRefreshSchedulerLeader(false)
		accountcontrol.SetRefreshService(nil)
	})
	appMainAcquireSchedulerFileLock = func(context.Context) (Hook, error) {
		lockAcquired = true
		return func(context.Context) error {
			lockReleased = true
			return nil
		}, nil
	}

	cleanup, err := defaultAppMainStartRefreshRuntime(context.Background(), state)
	if err != nil {
		t.Fatalf("refresh runtime error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("refresh runtime did not register cleanup")
	}
	if !lockAcquired || !accountcontrol.IsRefreshSchedulerLeader() {
		t.Fatalf("redis failure should fall back to local scheduler leader")
	}
	if state.schedulerKey != nil {
		t.Fatalf("redis scheduler key should not be set after fallback")
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if !lockReleased {
		t.Fatalf("local scheduler lock was not released")
	}
}

func TestRefreshRuntimeStartsConsoleQuotaResetLoop(t *testing.T) {
	expiredAt := int64(1)
	repo := &lifecycleAccountRepository{snapshot: accountcontrol.RuntimeSnapshot{
		Items: []accountcontrol.AccountRecord{{
			Token:  "tok-console-reset",
			Pool:   "basic",
			Status: accountcontrol.AccountStatusActive,
			Quota: accountcontrol.AccountQuotaSet{
				Auto: accountcontrol.QuotaWindow{Remaining: 0, Total: 0, WindowSeconds: 0},
				Fast: accountcontrol.QuotaWindow{Remaining: 30, Total: 30, WindowSeconds: 86400},
				Expert: accountcontrol.QuotaWindow{
					Remaining: 0,
					Total:     0,
				},
				Console: &accountcontrol.QuotaWindow{Remaining: 1, Total: 30, WindowSeconds: 900, ResetAt: &expiredAt},
			}.ToDict(),
		}},
	}}
	state := &appMainLifecycleState{repository: repo}
	oldAcquireLocalLock := appMainAcquireSchedulerFileLock
	oldConsoleResetInterval := appMainConsoleResetInterval
	t.Cleanup(func() {
		appMainAcquireSchedulerFileLock = oldAcquireLocalLock
		appMainConsoleResetInterval = oldConsoleResetInterval
		accountcontrol.SetRefreshScheduler(nil)
		accountcontrol.SetRefreshSchedulerLeader(false)
		accountcontrol.SetRefreshService(nil)
	})
	appMainAcquireSchedulerFileLock = func(context.Context) (Hook, error) {
		return func(context.Context) error { return nil }, nil
	}
	appMainConsoleResetInterval = time.Millisecond

	cleanup, err := defaultAppMainStartRefreshRuntime(context.Background(), state)

	if err != nil {
		t.Fatalf("refresh runtime error: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(repo.patches) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if len(repo.patches) == 0 {
		t.Fatalf("console quota reset loop did not patch expired console quota")
	}
	if repo.patches[0].QuotaConsole["remaining"] != 20 ||
		repo.patches[0].QuotaConsole["total"] != 20 ||
		repo.patches[0].QuotaConsole["window_seconds"] != 3600 ||
		repo.patches[0].QuotaConsole["reset_at"] != nil {
		t.Fatalf("console reset patch = %#v", repo.patches[0].QuotaConsole)
	}
}

func TestSchedulerLeaderLeaseStopsSchedulersWhenRenewLosesOwnership(t *testing.T) {
	renewed := make(chan struct{}, 2)
	lease := &fakeSchedulerLease{
		renew: func(context.Context) (bool, error) {
			renewed <- struct{}{}
			return false, nil
		},
	}
	accountcontrol.SetRefreshSchedulerLeader(true)
	t.Cleanup(func() { accountcontrol.SetRefreshSchedulerLeader(false) })

	cleanup := appMainStartSchedulerLeaderLeaseRenewal(
		context.Background(),
		lease,
		time.Millisecond,
	)

	select {
	case <-renewed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("lease renewal loop did not renew")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && accountcontrol.IsRefreshSchedulerLeader() {
		time.Sleep(time.Millisecond)
	}
	if accountcontrol.IsRefreshSchedulerLeader() {
		t.Fatal("scheduler leader flag should clear when lease renewal loses ownership")
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
}

func TestDefaultAccountDirectoryLifecycleBootstrapsAndSyncsLikePython(t *testing.T) {
	t.Setenv("ACCOUNT_SYNC_ACTIVE_INTERVAL", "0")
	t.Setenv("ACCOUNT_SYNC_INTERVAL", "1")
	repo := &lifecycleAccountRepository{
		snapshot: accountcontrol.RuntimeSnapshot{
			Revision: 1,
			Items: []accountcontrol.AccountRecord{
				lifecycleRuntimeRecord("tok-1", "basic", 3),
			},
		},
		changes: []accountcontrol.AccountChangeSet{{
			Revision: 2,
			Items: []accountcontrol.AccountRecord{
				lifecycleRuntimeRecord("tok-2", "super", 5),
			},
		}},
	}
	state := &appMainLifecycleState{repository: repo}

	cleanup, err := defaultAppMainStartAccountDirectory(context.Background(), state)
	if err != nil {
		t.Fatalf("account directory lifecycle returned error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("account directory lifecycle did not register sync cleanup")
	}
	if state.directory == nil {
		t.Fatalf("account directory lifecycle did not bind directory")
	}
	globalDirectory, err := accountdataplane.GetAccountDirectory(context.Background(), nil)
	if err != nil {
		t.Fatalf("account directory lifecycle did not initialise global directory: %v", err)
	}
	if globalDirectory != state.directory {
		t.Fatalf("global directory was not lifecycle directory")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && state.directory.Revision() != 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if state.directory.Size() != 2 || state.directory.Revision() != 2 {
		t.Fatalf("synced directory size/revision = %d/%d scanCalls=%#v", state.directory.Size(), state.directory.Revision(), repo.scanCalls)
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if len(repo.scanCalls) == 0 || repo.scanCalls[0] != 1 {
		t.Fatalf("scan revisions = %#v", repo.scanCalls)
	}
}

func TestDefaultAccountDirectoryLifecycleDoesNotBlockStartupOnLargeBootstrap(t *testing.T) {
	release := make(chan struct{})
	repo := &blockingLifecycleAccountRepository{
		entered: make(chan struct{}, 1),
		release: release,
		lifecycleAccountRepository: lifecycleAccountRepository{snapshot: accountcontrol.RuntimeSnapshot{
			Revision: 1,
			Items: []accountcontrol.AccountRecord{
				lifecycleRuntimeRecord("tok-1", "basic", 3),
			},
		}},
	}
	state := &appMainLifecycleState{repository: repo}

	cleanup, err := defaultAppMainStartAccountDirectory(context.Background(), state)
	if err != nil {
		t.Fatalf("account directory lifecycle returned error: %v", err)
	}
	if cleanup == nil || state.directory == nil {
		t.Fatalf("directory lifecycle did not bind cleanup/directory")
	}
	select {
	case <-repo.entered:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("bootstrap did not start in background")
	}
	if state.directory.Size() != 0 {
		t.Fatalf("directory should not be bootstrapped before release, size=%d", state.directory.Size())
	}
	close(release)
	waitForDirectoryRevision(t, state.directory, 1)
	if err := cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
}

func TestAppMainEnsureInitialAdminKeyUsesFixedDefaultAndPersistsMissingKey(t *testing.T) {
	data := map[string]any{"app": map[string]any{"app_key": ""}}
	backend := lifecycleConfigBackend{data: &data}
	previousConfig := config.GlobalConfig
	previousWriter := appMainInitialAdminKeyWriter
	var generatedKey string
	t.Cleanup(func() {
		config.GlobalConfig = previousConfig
		appMainInitialAdminKeyWriter = previousWriter
	})
	config.GlobalConfig = config.NewConfigSnapshot(backend, config.ConfigSnapshotOptions{})
	if err := config.GlobalConfig.Load(context.Background(), ""); err != nil {
		t.Fatalf("load config: %v", err)
	}
	appMainInitialAdminKeyWriter = func(key string) { generatedKey = key }

	if err := appMainEnsureInitialAdminKey(context.Background()); err != nil {
		t.Fatalf("ensure initial admin key: %v", err)
	}

	if generatedKey != appMainInitialAdminKey {
		t.Fatalf("generated key=%q, want %q", generatedKey, appMainInitialAdminKey)
	}
	if strings.TrimSpace(config.GetConfig("app.app_key", "").(string)) != generatedKey {
		t.Fatalf("global config app key was not reloaded")
	}
	appData, ok := data["app"].(map[string]any)
	if !ok || appData["app_key"] != generatedKey {
		t.Fatalf("backend app key not persisted: %#v", data)
	}
}

func waitForDirectoryRevision(t *testing.T, directory interface{ Revision() int }, revision int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && directory.Revision() < revision {
		time.Sleep(10 * time.Millisecond)
	}
}

type lifecycleConfigBackend struct {
	data *map[string]any
}

func (b lifecycleConfigBackend) Load(context.Context) (map[string]any, error) {
	if b.data == nil {
		return map[string]any{}, nil
	}
	return *b.data, nil
}
func (b lifecycleConfigBackend) ApplyPatch(_ context.Context, patch map[string]any) error {
	if b.data == nil {
		return nil
	}
	mergeLifecycleConfigData(*b.data, patch)
	return nil
}
func (lifecycleConfigBackend) Clear(context.Context) error          { return nil }
func (lifecycleConfigBackend) Version(context.Context) (any, error) { return 0, nil }
func (lifecycleConfigBackend) Close(context.Context) error          { return nil }

func mergeLifecycleConfigData(dst, patch map[string]any) {
	for key, value := range patch {
		nestedPatch, ok := value.(map[string]any)
		if !ok {
			dst[key] = value
			continue
		}
		nestedDst, _ := dst[key].(map[string]any)
		if nestedDst == nil {
			nestedDst = map[string]any{}
			dst[key] = nestedDst
		}
		mergeLifecycleConfigData(nestedDst, nestedPatch)
	}
}

type lifecycleAccountRepository struct {
	snapshot  accountcontrol.RuntimeSnapshot
	changes   []accountcontrol.AccountChangeSet
	scanCalls []int
	listQuery accountcontrol.ListAccountsQuery
	upserts   []accountcontrol.AccountUpsert
	patches   []accountcontrol.AccountPatch
	deleted   []string
}

type blockingLifecycleAccountRepository struct {
	lifecycleAccountRepository
	entered chan struct{}
	release <-chan struct{}
}

type fakeSchedulerLease struct {
	renew        func(context.Context) (bool, error)
	releaseCalls int
}

type lifecycleRuntimeRedis struct {
	locks      map[string]string
	setErr     error
	setKey     string
	setValue   string
	setPX      int
	deletedKey string
}

func (r *lifecycleRuntimeRedis) Get(_ context.Context, key string) (any, error) {
	if r.locks == nil {
		return nil, nil
	}
	value, ok := r.locks[key]
	if !ok {
		return nil, nil
	}
	return value, nil
}

func (r *lifecycleRuntimeRedis) SetNX(_ context.Context, key, value string, ttlMS int) (bool, error) {
	r.setKey = key
	r.setValue = value
	r.setPX = ttlMS
	if r.setErr != nil {
		return false, r.setErr
	}
	if r.locks == nil {
		return false, nil
	}
	if _, exists := r.locks[key]; exists {
		return false, nil
	}
	r.locks[key] = value
	return true, nil
}

func (r *lifecycleRuntimeRedis) Expire(context.Context, string, int) error {
	return nil
}

func (r *lifecycleRuntimeRedis) Delete(_ context.Context, key string) error {
	r.deletedKey = key
	if r.locks != nil {
		delete(r.locks, key)
	}
	return nil
}

func (r *lifecycleRuntimeRedis) CompareExpire(_ context.Context, key string, owner string, _ int) (bool, error) {
	if r.locks == nil || r.locks[key] != owner {
		return false, nil
	}
	return true, nil
}

func (r *lifecycleRuntimeRedis) CompareDelete(_ context.Context, key string, owner string) (bool, error) {
	if r.locks == nil || r.locks[key] != owner {
		return false, nil
	}
	r.deletedKey = key
	delete(r.locks, key)
	return true, nil
}

func (f *fakeSchedulerLease) Renew(ctx context.Context) (bool, error) {
	if f.renew != nil {
		return f.renew(ctx)
	}
	return true, nil
}

func (f *fakeSchedulerLease) Release(context.Context) (bool, error) {
	f.releaseCalls++
	return true, nil
}

func (r *blockingLifecycleAccountRepository) RuntimeSnapshot(ctx context.Context) (accountcontrol.RuntimeSnapshot, error) {
	if r.entered == nil {
		r.entered = make(chan struct{}, 1)
	}
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
	case <-ctx.Done():
		return accountcontrol.RuntimeSnapshot{}, ctx.Err()
	}
	return r.snapshot, nil
}

func (r *lifecycleAccountRepository) Initialize(context.Context) error { return nil }
func (r *lifecycleAccountRepository) GetRevision(context.Context) (int, error) {
	return r.snapshot.Revision, nil
}
func (r *lifecycleAccountRepository) RuntimeSnapshot(context.Context) (accountcontrol.RuntimeSnapshot, error) {
	return r.snapshot, nil
}
func (r *lifecycleAccountRepository) ScanChanges(_ context.Context, revision int, _ int) (accountcontrol.AccountChangeSet, error) {
	r.scanCalls = append(r.scanCalls, revision)
	if len(r.changes) == 0 {
		return accountcontrol.NewAccountChangeSet(), nil
	}
	next := r.changes[0]
	r.changes = r.changes[1:]
	return next, nil
}
func (r *lifecycleAccountRepository) UpsertAccounts(_ context.Context, items []accountcontrol.AccountUpsert) (accountcontrol.AccountMutationResult, error) {
	r.upserts = slices.Clone(items)
	return accountcontrol.AccountMutationResult{Upserted: len(items)}, nil
}
func (r *lifecycleAccountRepository) PatchAccounts(_ context.Context, patches []accountcontrol.AccountPatch) (accountcontrol.AccountMutationResult, error) {
	r.patches = slices.Clone(patches)
	return accountcontrol.AccountMutationResult{Patched: len(patches)}, nil
}
func (r *lifecycleAccountRepository) DeleteAccounts(_ context.Context, tokens []string) (accountcontrol.AccountMutationResult, error) {
	r.deleted = slices.Clone(tokens)
	return accountcontrol.AccountMutationResult{Deleted: len(tokens)}, nil
}
func (r *lifecycleAccountRepository) GetAccounts(context.Context, []string) ([]accountcontrol.AccountRecord, error) {
	return nil, errors.New("not used")
}
func (r *lifecycleAccountRepository) ListAccounts(_ context.Context, query accountcontrol.ListAccountsQuery) (accountcontrol.AccountPage, error) {
	r.listQuery = query
	return accountcontrol.AccountPage{
		Items:      slices.Clone(r.snapshot.Items),
		Total:      len(r.snapshot.Items),
		Page:       query.Page,
		PageSize:   query.PageSize,
		TotalPages: 1,
		Revision:   r.snapshot.Revision,
	}, nil
}
func (r *lifecycleAccountRepository) ReplacePool(context.Context, accountcontrol.BulkReplacePoolCommand) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{}, nil
}
func (r *lifecycleAccountRepository) Close(context.Context) error { return nil }

func lifecycleRuntimeRecord(token string, pool string, fastRemaining int) accountcontrol.AccountRecord {
	return accountcontrol.AccountRecord{
		Token:  token,
		Pool:   pool,
		Status: accountcontrol.AccountStatusActive,
		Quota: accountcontrol.AccountQuotaSet{
			Auto:   accountcontrol.QuotaWindow{Remaining: 1, Total: 10, WindowSeconds: 60},
			Fast:   accountcontrol.QuotaWindow{Remaining: fastRemaining, Total: 10, WindowSeconds: 60},
			Expert: accountcontrol.QuotaWindow{Remaining: 1, Total: 10, WindowSeconds: 60},
		}.ToDict(),
	}
}
