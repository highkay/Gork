package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

type vulnSummary struct {
	Findings       int `json:"findings"`
	CalledFindings int `json:"called_findings"`
	ImportFindings int `json:"import_findings"`
}

func main() {
	input := flag.String("input", "govulncheck.json", "govulncheck -json output path")
	flag.Parse()

	file, err := os.Open(*input)
	if err != nil {
		fail("open govulncheck output: %v", err)
	}
	defer file.Close()

	summary, err := summarize(file)
	if err != nil {
		fail("summarize govulncheck output: %v", err)
	}
	fmt.Printf("govulncheck findings=%d called=%d import_only=%d\n", summary.Findings, summary.CalledFindings, summary.ImportFindings)
	data, _ := json.Marshal(summary)
	fmt.Println(string(data))
}

func summarize(reader io.Reader) (vulnSummary, error) {
	decoder := json.NewDecoder(reader)
	summary := vulnSummary{}
	for {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return summary, err
		}
		finding, ok := event["finding"].(map[string]any)
		if !ok {
			continue
		}
		summary.Findings++
		if trace, ok := finding["trace"].([]any); ok && len(trace) > 0 {
			summary.CalledFindings++
		} else {
			summary.ImportFindings++
		}
	}
	return summary, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
