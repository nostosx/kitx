package ttlcache_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/nostosx/kitx/ttlcache"
)

func BenchmarkSetFull(b *testing.B) {
	for _, capacity := range []int{64, 10000} {
		for _, ttl := range []time.Duration{0, time.Hour} {
			b.Run(fmt.Sprintf("capacity=%d/ttl=%s", capacity, ttl), func(b *testing.B) {
				c, err := ttlcache.New[int, int](capacity)
				if err != nil {
					b.Fatal(err)
				}
				for key := range capacity {
					c.Set(key, key, ttl)
				}
				b.ReportAllocs()
				key := capacity
				for b.Loop() {
					c.Set(key, key, ttl)
					key++
				}
			})
		}
	}
}
