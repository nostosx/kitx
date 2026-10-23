package backoff_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/nostosx/kitx/backoff"
)

func TestExponential(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name           string
		initial, limit time.Duration
		attempt        int
		want           time.Duration
	}{
		{"first", time.Second, time.Minute, 1, time.Second},
		{"doubles", time.Second, time.Minute, 4, 8 * time.Second},
		{"capped", time.Second, 7 * time.Second, 4, 7 * time.Second},
		{"initial exceeds limit", time.Minute, time.Second, 1, time.Second},
		{"zero attempt", time.Second, time.Minute, 0, time.Second},
		{"negative attempt", time.Second, time.Minute, -1, time.Second},
		{"zero initial", 0, time.Second, 1, 0},
		{"negative initial", -1, time.Second, 2, 0},
		{"zero limit", time.Second, 0, 1, 0},
		{"negative limit", time.Second, -1, 1, 0},
		{"duration overflow", math.MaxInt64/2 + 1, math.MaxInt64, 2, math.MaxInt64},
		{"huge attempt", 1, math.MaxInt64, math.MaxInt, math.MaxInt64},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := backoff.Exponential(tt.initial, tt.limit)(tt.attempt); got != tt.want {
				t.Fatalf("delay = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFullJitter(t *testing.T) {
	t.Parallel()
	if got := backoff.FullJitter(nil)(1); got != 0 {
		t.Fatalf("nil delay = %v", got)
	}
	for _, bound := range []time.Duration{-1, 0, 1, time.Second, math.MaxInt64} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			delay := backoff.FullJitter(func(attempt int) time.Duration {
				if attempt != 3 {
					t.Errorf("attempt = %d, want 3", attempt)
				}
				return bound
			})
			for range 100 {
				got := delay(3)
				if bound <= 0 && got != 0 || bound > 0 && (got < 0 || got >= bound) {
					t.Fatalf("delay = %v, outside bound %v", got, bound)
				}
			}
		})
	}
}

func FuzzExponential(f *testing.F) {
	f.Add(int64(1), int64(math.MaxInt64), int(100))
	f.Add(int64(math.MaxInt64/2+1), int64(math.MaxInt64), int(2))
	f.Fuzz(func(t *testing.T, initial, limit int64, attempt int) {
		delay := backoff.Exponential(time.Duration(initial), time.Duration(limit))
		got := delay(attempt)
		if initial <= 0 || limit <= 0 {
			if got != 0 {
				t.Fatalf("disabled delay = %v", got)
			}
			return
		}
		if got < time.Duration(min(initial, limit)) || got > time.Duration(limit) {
			t.Fatalf("delay = %v, initial = %d, limit = %d", got, initial, limit)
		}
		if attempt < math.MaxInt && delay(attempt+1) < got {
			t.Fatal("delay decreased as attempt increased")
		}
	})
}

func ExampleExponential() {
	delay := backoff.Exponential(time.Second, 5*time.Second)
	for attempt := 1; attempt <= 5; attempt++ {
		fmt.Println(delay(attempt))
	}
	// Output:
	// 1s
	// 2s
	// 4s
	// 5s
	// 5s
}
