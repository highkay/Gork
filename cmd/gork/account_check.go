package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func runGorkCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	if len(args) == 0 {
		return false, 0, nil
	}
	if args[0] == "healthcheck" {
		return runHealthcheckCommand(ctx, args[1:], stdout, stderr)
	}
	if args[0] == "protocol-check" {
		return runProtocolCheckCommand(ctx, args[1:], stdout, stderr)
	}
	if args[0] == "config" {
		return runConfigCommand(ctx, args[1:], stdout, stderr)
	}
	if args[0] != "account" || len(args) < 2 {
		return true, 2, fmt.Errorf("unknown command: %s", strings.Join(args, " "))
	}
	switch args[1] {
	case "check":
		return runAccountCheckCommand(ctx, args[2:], stdout, stderr)
	case "sso-sweep":
		return runAccountSSOSweepCommand(ctx, args[2:], stdout, stderr)
	default:
		return true, 2, fmt.Errorf("unknown command: %s", strings.Join(args, " "))
	}
}
