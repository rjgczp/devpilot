package password

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

func TestPHCAndIndependentSalts(t *testing.T) {
	h := New()
	password := "  密码 exact 🔐 phrase  "
	a, err := h.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || strings.Contains(a, password) {
		t.Fatal("hash or salt not independent")
	}
	for _, encoded := range []string{a, b} {
		parts := strings.Split(encoded, "$")
		if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=1" {
			t.Fatal("incorrect PHC header")
		}
		salt, e1 := base64.RawStdEncoding.Strict().DecodeString(parts[4])
		key, e2 := base64.RawStdEncoding.Strict().DecodeString(parts[5])
		if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
			t.Fatal("incorrect PHC payload")
		}
		if !bytes.Equal(key, argon2.IDKey([]byte(password), salt, 3, 65536, 1, 32)) {
			t.Fatal("hash did not preserve password")
		}
		if bytes.Equal(key, argon2.IDKey([]byte("different password"), salt, 3, 65536, 1, 32)) {
			t.Fatal("wrong password matched")
		}
	}
}

type brokenRandom struct{}

func (brokenRandom) Read([]byte) (int, error) { return 0, errors.New("sentinel private RNG failure") }
func TestRandomFailure(t *testing.T) {
	for _, random := range []io.Reader{brokenRandom{}, strings.NewReader("short")} {
		h := &Hasher{random: random}
		hash, err := h.Hash("sentinel private password")
		if err == nil || hash != "" || strings.Contains(err.Error(), "sentinel") {
			t.Fatal("unsafe random failure")
		}
	}
}

func BenchmarkHash(b *testing.B) {
	h := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := h.Hash("benchmark password exact"); err != nil {
			b.Fatal(err)
		}
	}
}

// Batch concurrency is fixed, so measurements represent the two-slot envelope.
func BenchmarkHashResources(b *testing.B) {
	for _, concurrency := range []int{1, 2} {
		b.Run(fmt.Sprint(concurrency), func(b *testing.B) {
			h := New()
			samples := make([]time.Duration, 0, b.N*concurrency)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				durations := make([]time.Duration, concurrency)
				var wg sync.WaitGroup
				for j := 0; j < concurrency; j++ {
					wg.Add(1)
					go func(j int) {
						defer wg.Done()
						started := time.Now()
						_, err := h.Hash("benchmark password phrase")
						if err != nil {
							b.Error(err)
						}
						durations[j] = time.Since(started)
					}(j)
				}
				wg.Wait()
				samples = append(samples, durations...)
			}
			b.StopTimer()
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			b.ReportMetric(float64(samples[(len(samples)-1)/2].Microseconds())/1000, "p50-ms")
			b.ReportMetric(float64(samples[(len(samples)-1)*95/100].Microseconds())/1000, "p95-ms")
		})
	}
}
