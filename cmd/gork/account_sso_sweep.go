package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountbackends "github.com/dslzl/gork/app/control/account/backends"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reversetransport "github.com/dslzl/gork/app/dataplane/reverse/transport"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type ssoSweepStats struct {
	Checked        atomic.Int64
	SessionOK      atomic.Int64
	SessionInvalid atomic.Int64
	LocalInvalid   atomic.Int64
	Cloudflare     atomic.Int64
	RateLimited    atomic.Int64
	HTTPBlock      atomic.Int64
	OtherFail      atomic.Int64
	Deleted        atomic.Int64
	DeleteErrors   atomic.Int64

	sampleMu     sync.Mutex
	sampleErrors []string
}

func runAccountSSOSweepCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	flags := flag.NewFlagSet("account sso-sweep", flag.ContinueOnError)
	flags.SetOutput(stderr)
	concurrency := flags.Int("concurrency", 8, "probe concurrency")
	limit := flags.Int("limit", 0, "max accounts to probe (0 = all)")
	offset := flags.Int("offset", 0, "skip first N active accounts")
	dryRun := flags.Bool("dry-run", false, "probe only; do not delete")
	adminURL := flags.String("admin-url", "http://127.0.0.1:8000", "admin base URL for deletes")
	adminAuth := flags.String("admin-auth", "gork", "admin bearer token")
	batchSize := flags.Int("delete-batch", 100, "admin delete batch size")
	pageSize := flags.Int("page-size", 500, "repository page size while listing")
	progressEvery := flags.Int("progress-every", 100, "progress log interval")
	if err := flags.Parse(args); err != nil {
		return true, 2, err
	}
	if flags.NArg() != 0 {
		return true, 2, fmt.Errorf("unexpected account sso-sweep argument: %s", strings.Join(flags.Args(), " "))
	}
	if *concurrency < 1 {
		*concurrency = 1
	}
	if *pageSize < 1 {
		*pageSize = 500
	}

	if err := platformconfig.GlobalConfig.EnsureLoaded(ctx, ""); err != nil {
		return true, 1, fmt.Errorf("load config: %w", err)
	}

	repo, err := accountbackends.CreateRepository(commandEnv(), accountbackends.RepositoryConstructors{})
	if err != nil {
		return true, 1, err
	}
	if err := repo.Initialize(ctx); err != nil {
		_ = repo.Close(ctx)
		return true, 1, err
	}
	defer func() { _ = repo.Close(ctx) }()

	proxyRuntime, err := proxydataplane.GetTransportRuntime(ctx)
	if err != nil {
		return true, 1, fmt.Errorf("proxy runtime: %w", err)
	}
	prober := reversetransport.SSOSessionProber{ProxyRuntime: proxyRuntime}

	tokens, err := listActiveTokensForSweep(ctx, repo, *pageSize, *offset, *limit)
	if err != nil {
		return true, 1, err
	}
	fmt.Fprintf(stdout, "sso-sweep start tokens=%d concurrency=%d dry_run=%v admin=%s\n",
		len(tokens), *concurrency, *dryRun, *adminURL)

	stats := &ssoSweepStats{}
	invalidCh := make(chan string, *concurrency*4)
	var deleteWG sync.WaitGroup
	if !*dryRun {
		deleteWG.Add(1)
		go func() {
			defer deleteWG.Done()
			runSSOSweepDeleter(ctx, *adminURL, *adminAuth, *batchSize, invalidCh, stats, stdout)
		}()
	} else {
		go func() {
			for range invalidCh {
			}
		}()
	}

	jobs := make(chan string)
	var workers sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for token := range jobs {
				if ctx.Err() != nil {
					return
				}
				classifyAndQueue(ctx, prober, token, invalidCh, stats, stdout)
				n := stats.Checked.Add(1)
				if *progressEvery > 0 && n%int64(*progressEvery) == 0 {
					printSSOSweepProgress(stdout, stats, len(tokens))
				}
			}
		}()
	}

	for _, token := range tokens {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			close(invalidCh)
			deleteWG.Wait()
			return true, 1, ctx.Err()
		case jobs <- token:
		}
	}
	close(jobs)
	workers.Wait()
	close(invalidCh)
	deleteWG.Wait()

	printSSOSweepProgress(stdout, stats, len(tokens))
	fmt.Fprintf(stdout, "sso-sweep done checked=%d ok=%d session_invalid=%d local_invalid=%d cf=%d rate=%d http_block=%d other=%d deleted=%d delete_errors=%d dry_run=%v\n",
		stats.Checked.Load(),
		stats.SessionOK.Load(),
		stats.SessionInvalid.Load(),
		stats.LocalInvalid.Load(),
		stats.Cloudflare.Load(),
		stats.RateLimited.Load(),
		stats.HTTPBlock.Load(),
		stats.OtherFail.Load(),
		stats.Deleted.Load(),
		stats.DeleteErrors.Load(),
		*dryRun,
	)
	return true, 0, nil
}

func listActiveTokensForSweep(ctx context.Context, repo accountcontrol.AccountRefreshRepository, pageSize, offset, limit int) ([]string, error) {
	tokens := []string{}
	page := 1
	skipped := 0
	active := accountcontrol.AccountStatusActive
	for {
		accountPage, err := repo.ListAccounts(ctx, accountcontrol.ListAccountsQuery{
			Page:           page,
			PageSize:       pageSize,
			IncludeDeleted: false,
			Status:         &active,
			SortBy:         "token",
			SortDesc:       false,
		})
		if err != nil {
			return nil, err
		}
		if len(accountPage.Items) == 0 {
			break
		}
		for _, item := range accountPage.Items {
			if item.IsDeleted() || item.Status != accountcontrol.AccountStatusActive {
				continue
			}
			if skipped < offset {
				skipped++
				continue
			}
			tokens = append(tokens, item.Token)
			if limit > 0 && len(tokens) >= limit {
				return tokens, nil
			}
		}
		if accountPage.TotalPages > 0 && page >= accountPage.TotalPages {
			break
		}
		page++
	}
	return tokens, nil
}

func classifyAndQueue(ctx context.Context, prober reversetransport.SSOSessionProber, token string, invalidCh chan<- string, stats *ssoSweepStats, stdout io.Writer) {
	if reason := protocol.SSOLocalInvalidReason(token, time.Now()); reason != "" {
		stats.LocalInvalid.Add(1)
		select {
		case invalidCh <- token:
		case <-ctx.Done():
		}
		return
	}
	err := prober.ProbeSession(ctx, token)
	if err == nil {
		stats.SessionOK.Add(1)
		return
	}
	if protocol.IsSessionInvalidError(err) {
		stats.SessionInvalid.Add(1)
		select {
		case invalidCh <- token:
		case <-ctx.Done():
		}
		return
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "cloudflare"):
		stats.Cloudflare.Add(1)
	case strings.Contains(msg, "rate limited"):
		stats.RateLimited.Add(1)
	case strings.Contains(msg, "http block"):
		stats.HTTPBlock.Add(1)
	default:
		stats.OtherFail.Add(1)
		stats.sampleMu.Lock()
		if len(stats.sampleErrors) < 8 {
			sample := err.Error()
			if len(sample) > 240 {
				sample = sample[:240]
			}
			stats.sampleErrors = append(stats.sampleErrors, sample)
			fmt.Fprintf(stdout, "sample_other_error: %s\n", sample)
		}
		stats.sampleMu.Unlock()
	}
}

func runSSOSweepDeleter(ctx context.Context, adminURL, adminAuth string, batchSize int, invalidCh <-chan string, stats *ssoSweepStats, stdout io.Writer) {
	if batchSize < 1 {
		batchSize = 100
	}
	buf := make([]string, 0, batchSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		n, err := adminDeleteTokens(ctx, adminURL, adminAuth, buf)
		if err != nil {
			stats.DeleteErrors.Add(1)
			fmt.Fprintf(stdout, "delete error: %v (batch=%d)\n", err, len(buf))
		} else {
			stats.Deleted.Add(int64(n))
		}
		buf = buf[:0]
	}
	for token := range invalidCh {
		buf = append(buf, token)
		if len(buf) >= batchSize {
			flush()
		}
	}
	flush()
}

func adminDeleteTokens(ctx context.Context, adminURL, adminAuth string, tokens []string) (int, error) {
	body, err := json.Marshal(tokens)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(adminURL, "/")+"/admin/api/tokens", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+adminAuth)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return len(tokens), nil
	}
	if v, ok := parsed["deleted"].(float64); ok {
		return int(v), nil
	}
	return len(tokens), nil
}

func printSSOSweepProgress(w io.Writer, stats *ssoSweepStats, total int) {
	fmt.Fprintf(w, "progress checked=%d/%d ok=%d invalid=%d local=%d cf=%d rate=%d block=%d other=%d deleted=%d\n",
		stats.Checked.Load(), total,
		stats.SessionOK.Load(),
		stats.SessionInvalid.Load(),
		stats.LocalInvalid.Load(),
		stats.Cloudflare.Load(),
		stats.RateLimited.Load(),
		stats.HTTPBlock.Load(),
		stats.OtherFail.Load(),
		stats.Deleted.Load(),
	)
}
