package ttlcache_test

import (
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/ttlcache"
)

func TestNewUnboundedAndClock(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := ttlcache.NewUnbounded[int, int](ttlcache.WithClock(func() time.Time { return now }), nil)
	for key := range 10001 {
		c.Set(key, key, time.Second)
	}
	if n := c.Len(); n != 10001 {
		t.Fatalf("unbounded cache lost entries: %d", n)
	}
	now = now.Add(time.Second)
	if n := c.PurgeExpired(); n != 10001 || c.Len() != 0 {
		t.Fatalf("expiry at injected deadline: purged=%d, len=%d", n, c.Len())
	}
	c.Clear()
	c.Set(1, 42, 0)
	if v, ok := c.Get(1); !ok || v != 42 {
		t.Fatalf("unbounded reuse: %d, %v", v, ok)
	}
}

func TestWithNilClockKeepsSystemClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := ttlcache.NewUnbounded[string, int](ttlcache.WithClock(nil))
		c.Set("key", 1, time.Second)
		synctest.Sleep(time.Second)
		if _, ok := c.Get("key"); ok {
			t.Fatal("nil clock disabled expiry")
		}
	})
}

func TestSetUsesExpiredEntryBeforeLiveLRU(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c, err := ttlcache.New[string, int](3, ttlcache.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	c.Set("live", 1, 0)
	c.Set("later", 2, time.Minute)
	c.Set("soon", 3, time.Second)
	now = now.Add(time.Hour)
	c.Set("new", 4, 0)
	if v, ok := c.Peek("live"); !ok || v != 1 {
		t.Fatal("live LRU was evicted before an expired entry")
	}
	if n := c.PurgeExpired(); n != 1 {
		t.Fatalf("full insertion should reclaim one slot, remaining expired=%d", n)
	}
}

func ExampleNewUnbounded() {
	c := ttlcache.NewUnbounded[string, int]()
	c.Set("count", 42, time.Minute)
	fmt.Println(c.Len())
	// Output: 1
}
