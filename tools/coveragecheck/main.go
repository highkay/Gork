package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type packageCoverage struct {
	Covered int64   `json:"covered"`
	Total   int64   `json:"total"`
	Percent float64 `json:"percent"`
}

type report map[string]packageCoverage

var softMinimums = map[string]float64{
	"./app/products":                 10,
	"./app/platform/config/backends": 10,
	"./app/platform/storage":         20,
	"./app/products/anthropic":       20,
	"./app/products/openai":          20,
	"./app/products/web/admin":       20,
}

func main() {
	profile := flag.String("profile", "coverage.out", "coverage profile path")
	baseline := flag.String("baseline", ".github/coverage-baseline.json", "optional baseline JSON path")
	out := flag.String("out", "coverage-summary.json", "summary JSON path")
	maxDrop := flag.Float64("max-drop", 2.0, "maximum allowed coverage drop when baseline exists")
	flag.Parse()

	current, err := readCoverageProfile(*profile)
	if err != nil {
		fail("read coverage profile: %v", err)
	}
	if err := writeReport(*out, current); err != nil {
		fail("write coverage summary: %v", err)
	}
	printReport(current)
	printSoftThresholds(current)

	previous, err := readOptionalReport(*baseline)
	if err != nil {
		fail("read coverage baseline: %v", err)
	}
	if previous == nil {
		fmt.Printf("coverage baseline %q not found; trend check skipped\n", *baseline)
		return
	}
	if failed := compareTrend(previous, current, *maxDrop); failed {
		os.Exit(1)
	}
}

func readCoverageProfile(profilePath string) (report, error) {
	file, err := os.Open(profilePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	coverage := report{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		filePath, statements, count, ok := parseProfileLine(line)
		if !ok {
			continue
		}
		pkg := packageName(filePath)
		item := coverage[pkg]
		item.Total += int64(statements)
		if count > 0 {
			item.Covered += int64(statements)
		}
		coverage[pkg] = item
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for key, item := range coverage {
		if item.Total > 0 {
			item.Percent = float64(item.Covered) * 100 / float64(item.Total)
		}
		coverage[key] = item
	}
	return coverage, nil
}

func parseProfileLine(line string) (string, int, int, bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", 0, 0, false
	}
	location := fields[0]
	colon := strings.Index(location, ":")
	if colon <= 0 {
		return "", 0, 0, false
	}
	statements, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, 0, false
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, 0, false
	}
	return location[:colon], statements, count, true
}

func packageName(filePath string) string {
	clean := normalizeProfilePath(filePath)
	dir := path.Dir(clean)
	if dir == "." {
		return "."
	}
	return "./" + dir
}

func normalizeProfilePath(filePath string) string {
	clean := strings.ReplaceAll(filePath, "\\", "/")
	if filepath.IsAbs(filePath) {
		if rel, err := filepath.Rel(mustGetwd(), filePath); err == nil && !strings.HasPrefix(rel, "..") {
			clean = strings.ReplaceAll(rel, "\\", "/")
		}
	}
	if module := modulePath(); module != "" && strings.HasPrefix(clean, module+"/") {
		clean = strings.TrimPrefix(clean, module+"/")
	}
	return strings.TrimPrefix(clean, "./")
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func modulePath() string {
	dir := mustGetwd()
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "module ") {
					return strings.TrimSpace(strings.TrimPrefix(line, "module "))
				}
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func writeReport(path string, coverage report) error {
	data, err := json.MarshalIndent(coverage, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func readOptionalReport(path string) (report, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var coverage report
	if err := json.Unmarshal(data, &coverage); err != nil {
		return nil, err
	}
	return coverage, nil
}

func printReport(coverage report) {
	for _, pkg := range sortedPackages(coverage) {
		item := coverage[pkg]
		fmt.Printf("%-55s %6.2f%% (%d/%d)\n", pkg, item.Percent, item.Covered, item.Total)
	}
}

func compareTrend(previous, current report, maxDrop float64) bool {
	failed := false
	for _, pkg := range sortedPackages(previous) {
		oldItem := previous[pkg]
		newItem, ok := current[pkg]
		if !ok || oldItem.Total == 0 {
			continue
		}
		drop := oldItem.Percent - newItem.Percent
		if drop > maxDrop {
			fmt.Printf("coverage drop too large: %s %.2f%% -> %.2f%% (drop %.2f%%)\n", pkg, oldItem.Percent, newItem.Percent, drop)
			failed = true
		}
	}
	return failed
}

func printSoftThresholds(coverage report) {
	for _, pkg := range sortedThresholdPackages() {
		minimum := softMinimums[pkg]
		item, ok := coverage[pkg]
		if !ok {
			fmt.Printf("coverage soft threshold warning: %s missing from profile (target %.2f%%)\n", pkg, minimum)
			continue
		}
		if item.Percent < minimum {
			fmt.Printf("coverage soft threshold warning: %s %.2f%% below %.2f%%\n", pkg, item.Percent, minimum)
		}
	}
}

func sortedThresholdPackages() []string {
	packages := make([]string, 0, len(softMinimums))
	for pkg := range softMinimums {
		packages = append(packages, pkg)
	}
	slices.Sort(packages)
	return packages
}

func sortedPackages(coverage report) []string {
	packages := make([]string, 0, len(coverage))
	for pkg := range coverage {
		packages = append(packages, pkg)
	}
	slices.Sort(packages)
	return packages
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
