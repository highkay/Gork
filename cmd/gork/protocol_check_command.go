package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	reverse "github.com/dslzl/gork/app/dataplane/reverse"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

func runProtocolCheckCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	flags := flag.NewFlagSet("protocol-check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write JSON output")
	targetList := flags.String("target", "", "comma-separated targets")
	if err := flags.Parse(args); err != nil {
		return true, 2, err
	}
	if flags.NArg() != 0 {
		return true, 2, fmt.Errorf("unexpected protocol-check argument: %s", strings.Join(flags.Args(), " "))
	}
	targets := []string{}
	if *targetList != "" {
		targets = strings.Split(*targetList, ",")
	}
	results := reverse.RunProtocolCheck(ctx, targets, reverse.EndpointProtocolChecker{Endpoints: reverseruntime.GlobalEndpointTable()})
	if *jsonOutput {
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
	if len(results) == 0 {
		return 1
	}
	for _, result := range results {
		if result.Status != "ok" {
			return 1
		}
	}
	return 0
}
