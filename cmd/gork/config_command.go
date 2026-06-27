package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

func runConfigCommand(_ context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	if len(args) == 0 {
		return true, 2, fmt.Errorf("missing config subcommand")
	}
	switch args[0] {
	case "validate":
		return runConfigValidateCommand(args[1:], stdout, stderr)
	case "docs":
		return runConfigDocsCommand(args[1:], stdout, stderr)
	default:
		return true, 2, fmt.Errorf("unknown config subcommand: %s", args[0])
	}
}

func runConfigValidateCommand(args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	flags := flag.NewFlagSet("config validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultsPath := flags.String("defaults", platformconfig.ResolveDefaultsPath(), "defaults TOML path")
	configPath := flags.String("config", "", "user config TOML path")
	jsonOutput := flags.Bool("json", false, "print JSON issues")
	if err := flags.Parse(args); err != nil {
		return true, 2, err
	}
	if flags.NArg() != 0 {
		return true, 2, fmt.Errorf("unexpected config validate argument: %s", strings.Join(flags.Args(), " "))
	}
	defaults, err := platformconfig.LoadTOML(*defaultsPath)
	if err != nil {
		return true, 1, err
	}
	issues := []platformconfig.ConfigValidationIssue{}
	if validation := platformconfig.ValidateConfigData(defaults, defaults); validation != nil {
		issues = append(issues, validation.Issues...)
	}
	if strings.TrimSpace(*configPath) != "" {
		user, err := platformconfig.LoadTOML(*configPath)
		if err != nil {
			return true, 1, err
		}
		if validation := platformconfig.ValidateConfigPatch(defaults, user); validation != nil {
			issues = append(issues, validation.Issues...)
		}
	}
	issues = append(issues, validateConfigEnv(defaults, "GROK_", commandEnv())...)
	if len(issues) > 0 {
		if *jsonOutput {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(map[string]any{"ok": false, "issues": issues}); err != nil {
				return true, 1, err
			}
		} else {
			for _, issue := range issues {
				fmt.Fprintf(stderr, "%s %s: %s\n", issue.Code, issue.Key, issue.Message)
			}
		}
		return true, 1, nil
	}
	if *jsonOutput {
		_, _ = fmt.Fprintln(stdout, `{"ok":true,"issues":[]}`)
	} else {
		_, _ = fmt.Fprintln(stdout, "ok")
	}
	return true, 0, nil
}

func runConfigDocsCommand(args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	flags := flag.NewFlagSet("config docs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultsPath := flags.String("defaults", platformconfig.ResolveDefaultsPath(), "defaults TOML path")
	if err := flags.Parse(args); err != nil {
		return true, 2, err
	}
	if flags.NArg() != 0 {
		return true, 2, fmt.Errorf("unexpected config docs argument: %s", strings.Join(flags.Args(), " "))
	}
	defaults, err := platformconfig.LoadTOML(*defaultsPath)
	if err != nil {
		return true, 1, err
	}
	_, _ = io.WriteString(stdout, platformconfig.RenderSchemaMarkdown(platformconfig.DefaultSchema(defaults)))
	return true, 0, nil
}

func validateConfigEnv(defaults map[string]any, prefix string, env map[string]string) []platformconfig.ConfigValidationIssue {
	knownEnv := map[string]platformconfig.ConfigSchemaEntry{}
	for _, entry := range platformconfig.DefaultSchema(defaults) {
		knownEnv[entry.Env] = entry
	}
	issues := []platformconfig.ConfigValidationIssue{}
	for key, value := range env {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, ok := knownEnv[key]
		if !ok {
			issues = append(issues, platformconfig.ConfigValidationIssue{Key: key, Code: "unknown_env", Message: "unknown env override"})
			continue
		}
		patch := map[string]any{}
		platformconfig.SetNested(patch, entry.Key, configEnvValidationValue(entry, value))
		if validation := platformconfig.ValidateConfigPatch(defaults, patch); validation != nil {
			issues = append(issues, validation.Issues...)
		}
	}
	return issues
}

func configEnvValidationValue(entry platformconfig.ConfigSchemaEntry, value string) any {
	if entry.Kind != platformconfig.ConfigKindStringList {
		return value
	}
	items := []any{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func configCommandEnv() map[string]string {
	out := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
