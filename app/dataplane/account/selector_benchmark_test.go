package account

import (
	"fmt"
	"testing"
)

func BenchmarkAccountSelection(b *testing.B) {
	table := MakeEmptyTable()
	for i := 0; i < 5000; i++ {
		table.AppendSlot(AccountSlot{
			Token:        fmt.Sprintf("tok-%05d", i),
			PoolID:       i % 4,
			StatusID:     StatusActive,
			QuotaFast:    100 - i%50,
			TotalFast:    100,
			WindowFast:   3600,
			ResetFast:    7200,
			Health:       1 - float64(i%10)/20,
			LastUseS:     i % 120,
			LastFailS:    i % 300,
			FailCount:    i % 3,
			Tags:         []string{fmt.Sprintf("bucket-%02d", i%16)},
			QuotaConsole: 100,
			TotalConsole: 100,
		})
	}
	if err := SetStrategy("quota"); err != nil {
		b.Fatalf("SetStrategy returned error: %v", err)
	}
	b.Cleanup(func() { _ = SetStrategy("random") })
	options := SelectOptions{NowS: 1000, MaxInflight: 8}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := Select(table, i%4, 1, options); !ok {
			b.Fatal("Select returned no account")
		}
	}
}
