package password

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"
)

// Hash and real-cost Verify share the same two-slot envelope. Run in isolated
// non-race processes for RSS evidence; race testing is a separate check.
func BenchmarkMixedResources(b *testing.B) {
	for _, concurrency := range []int{1, 2} {
		b.Run(fmt.Sprint(concurrency), func(b *testing.B) {
			h := New()
			budget := NewBudget()
			samples := make([]time.Duration, 0, b.N*concurrency)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				elapsed := make([]time.Duration, concurrency)
				var wg sync.WaitGroup
				for j := 0; j < concurrency; j++ {
					wg.Add(1)
					go func(j int) {
						defer wg.Done()
						if !budget.Acquire() {
							b.Error("benchmark exceeded shared budget")
							return
						}
						defer budget.Release()
						start := time.Now()
						if (i+j)%2 == 0 {
							if _, err := h.Hash("benchmark exact phrase"); err != nil {
								b.Error("benchmark hash failure")
							}
						} else {
							if matched, err := h.Verify("devpilot dummy credential", DummyPHC); err != nil || !matched {
								b.Error("benchmark verify failure")
							}
						}
						elapsed[j] = time.Since(start)
					}(j)
				}
				wg.Wait()
				samples = append(samples, elapsed...)
			}
			b.StopTimer()
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			b.ReportMetric(float64(len(samples)), "samples")
			b.ReportMetric(float64(samples[(len(samples)-1)/2].Microseconds())/1000, "p50-ms")
			b.ReportMetric(float64(samples[(len(samples)-1)*95/100].Microseconds())/1000, "p95-ms")
		})
	}
}
