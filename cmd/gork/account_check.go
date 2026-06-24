package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountbackends "github.com/dslzl/gork/app/control/account/backends"
	reverse "github.com/dslzl/gork/app/dataplane/reverse"
)

func runGorkCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	if len(args) == 0 {
		return false, 0, nil
	}
	if args[0] == "healthcheck" {
		return runHealthcheckCommand(ctx, args[1:], stdout, stderr)
	}
	if args[0] == "protocol-check" {
		return runProtocolCheckCommand(ctx, args[1:], stdout)
	}
	if args[0] != "account" || len(args) < 2 || args[1] != "check" {
		return true, 2, fmt.Errorf("unknown command: %s", strings.Join(args, " "))
	}
	jsonOutput := false
	for _, arg := range args[2:] {
		switch arg {
		case "--json":
			jsonOutput = true
		default:
			return true, 2, fmt.Errorf("unknown account check flag: %s", arg)
		}
	}
	report, err := runAccountCheck(ctx, accountbackends.RepositoryConstructors{})
	if err != nil {
		return true, 1, err
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return true, 1, err
		}
	} else {
		fmt.Fprintf(stdout, "ok=%t revision=%d snapshot=%d list=%d issues=%d\n", report.OK, report.Revision, report.SnapshotCount, report.ListCount, len(report.Issues))
		for _, issue := range report.Issues {
			fmt.Fprintf(stdout, "%s %s %s\n", issue.Code, issue.Token, issue.Message)
		}
	}
	_ = stderr
	if !report.OK {
		return true, 1, nil
	}
	return true, 0, nil
}

func runHealthcheckCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	url := flags.String("url", defaultHealthcheckURL(), "healthcheck URL")
	timeout := flags.Duration("timeout", 5*time.Second, "request timeout")
	if err := flags.Parse(args); err != nil {
		return true, 2, err
	}
	if flags.NArg() != 0 {
		return true, 2, fmt.Errorf("unexpected healthcheck argument: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*url) == "" {
		return true, 2, fmt.Errorf("healthcheck URL cannot be empty")
	}
	client := &http.Client{Timeout: *timeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		return true, 1, err
	}
	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
		return true, 1, nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(stderr, "healthcheck failed: status=%d\n", response.StatusCode)
		return true, 1, nil
	}
	fmt.Fprintln(stdout, "ok")
	return true, 0, nil
}

func defaultHealthcheckURL() string {
	if raw := strings.TrimSpace(os.Getenv("GORK_HEALTHCHECK_URL")); raw != "" {
		return raw
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("SERVER_PORT")
	}
	if port == "" {
		port = "8000"
	}
	return "http://127.0.0.1:" + port + "/health"
}

func runProtocolCheckCommand(ctx context.Context, args []string, stdout io.Writer) (bool, int, error) {
	jsonOutput := false
	targets := []string{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--target":
			if i+1 >= len(args) {
				return true, 2, fmt.Errorf("missing protocol-check --target value")
			}
			targets = strings.Split(args[i+1], ",")
			i++
		default:
			return true, 2, fmt.Errorf("unknown protocol-check flag: %s", args[i])
		}
	}
	results := reverse.RunProtocolCheck(ctx, targets, nil)
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(results); err != nil {
			return true, 1, err
		}
		return true, protocolCheckExitCode(results), nil
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "%s status=%s latency_ms=%d error_type=%s request_id=%s checked_at=%s\n",
			result.Target, result.Status, result.LatencyMS, result.ErrorType, result.RequestID, result.CheckedAt)
	}
	return true, protocolCheckExitCode(results), nil
}

func protocolCheckExitCode(results []reverse.ProtocolCheckResult) int {
	for _, result := range results {
		if result.Status != "ok" {
			return 1
		}
	}
	return 0
}

func runAccountCheck(ctx context.Context, constructors accountbackends.RepositoryConstructors) (accountcontrol.AccountConsistencyReport, error) {
	repo, err := accountbackends.CreateRepository(commandEnv(), constructors)
	if err != nil {
		return accountcontrol.AccountConsistencyReport{}, err
	}
	if err := repo.Initialize(ctx); err != nil {
		_ = repo.Close(ctx)
		return accountcontrol.AccountConsistencyReport{}, err
	}
	defer func() { _ = repo.Close(ctx) }()
	return accountcontrol.CheckAccountRepositoryConsistency(ctx, repo)
}

func commandEnv() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}
