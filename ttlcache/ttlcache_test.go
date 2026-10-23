package ttlcache_test

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nostosx/kitx/ttlcache"
)

func TestNewAndZeroValue(t *testing.T) {
	t.Parallel()
	for _, capacity := range []int{-1, 0} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			if c, err := ttlcache.New[string, int](capacity); c != nil || !errors.Is(err, ttlcache.ErrInvalidCapacity) {
				t.Fatalf("New = %v, %v", c, err)
			}
		})
	}
	var c ttlcache.Cache[string, int]
	c.Set("key", 42, 0)
	if value, ok := c.Get("key"); value != 0 || ok || c.Len() != 0 {
		t.Fatalf("zero cache = %d, %v, len %d", value, ok, c.Len())
	}
	c.Delete("key")
	c.Clear()
	if n := c.PurgeExpired(); n != 0 {
		t.Fatal(n)
	}
}

func TestGetLRUAndReplacement(t *testing.T) {
	t.Parallel()
	c := newCache[string, int](t, 2)
	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	if value, ok := c.Get("a"); !ok || value != 1 {
		t.Fatalf("Get(a) = %d, %v", value, ok)
	}
	c.Set("c", 3, 0)
	if _, ok := c.Get("b"); ok {
		t.Fatal("least recently used entry was retained")
	}
	c.Set("a", 4, 0)
	c.Set("d", 5, 0)
	if _, ok := c.Get("c"); ok {
		t.Fatal("Set did not refresh recency")
	}
	if value, ok := c.Get("a"); !ok || value != 4 || c.Len() != 2 {
		t.Fatalf("replacement = %d, %v, len %d", value, ok, c.Len())
	}
	c.Delete("a")
	c.Delete("absent")
	if _, ok := c.Get("a"); ok || c.Len() != 1 {
		t.Fatal("Delete did not remove entry")
	}
	c.Clear()
	c.Set("new", 6, 0)
	if value, ok := c.Get("new"); !ok || value != 6 || c.Len() != 1 {
		t.Fatalf("reuse = %d, %v", value, ok)
	}
}

func TestGetExpiresAtDeadlineWithoutSliding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string, int](t, 2)
		c.Set("key", 42, time.Second)
		time.Sleep(500 * time.Millisecond)
		if value, ok := c.Get("key"); value != 42 || !ok {
			t.Fatalf("before expiry = %d, %v", value, ok)
		}
		time.Sleep(500 * time.Millisecond)
		if value, ok := c.Get("key"); value != 0 || ok || c.Len() != 0 {
			t.Fatalf("at expiry = %d, %v", value, ok)
		}
	})
}

func TestSetResetsTTLAndSupportsNonexpiringValues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string, int](t, 3)
		c.Set("key", 1, time.Second)
		time.Sleep(500 * time.Millisecond)
		c.Set("key", 2, time.Second)
		c.Set("zero", 3, 0)
		c.Set("negative", 4, -time.Second)
		time.Sleep(500 * time.Millisecond)
		if value, ok := c.Get("key"); !ok || value != 2 {
			t.Fatalf("reset TTL = %d, %v", value, ok)
		}
		c.Set("key", 5, 0)
		time.Sleep(24 * time.Hour)
		if c.Len() != 3 {
			t.Fatal("nonexpiring entries expired")
		}
	})
}

func TestSetPurgesExpiredBeforeLiveEviction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string, int](t, 2)
		c.Set("live", 1, 0)
		c.Set("expiring", 2, time.Second)
		time.Sleep(time.Second)
		c.Set("new", 3, 0)
		if value, ok := c.Get("live"); !ok || value != 1 {
			t.Fatalf("live entry evicted ahead of expired one: %d, %v", value, ok)
		}
		if c.Len() != 2 {
			t.Fatal(c.Len())
		}
	})
}

func TestPurgeExpiredAndLen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[int, int](t, 4)
		c.Set(1, 1, time.Second)
		c.Set(2, 2, 2*time.Second)
		c.Set(3, 3, 0)
		time.Sleep(time.Second)
		if n := c.PurgeExpired(); n != 1 {
			t.Fatalf("PurgeExpired = %d", n)
		}
		if n := c.PurgeExpired(); n != 0 {
			t.Fatalf("repeated purge = %d", n)
		}
		time.Sleep(time.Second)
		if n := c.Len(); n != 1 {
			t.Fatalf("Len = %d", n)
		}
	})
}

func TestCacheStoresZeroAndSharedValues(t *testing.T) {
	t.Parallel()
	c := newCache[string, []byte](t, 2)
	c.Set("nil", nil, 0)
	if value, ok := c.Get("nil"); !ok || value != nil {
		t.Fatalf("nil value = %v, %v", value, ok)
	}
	data := []byte("a")
	c.Set("shared", data, 0)
	data[0] = 'b'
	if value, ok := c.Get("shared"); !ok || string(value) != "b" {
		t.Fatalf("shared value = %q, %v", value, ok)
	}
}

func TestCacheConcurrentUse(t *testing.T) {
	t.Parallel()
	c := newCache[int, int](t, 16)
	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Go(func() {
			for i := range 100 {
				key := worker*100 + i
				c.Set(key, key, time.Minute)
				if value, ok := c.Get(key); ok && value != key {
					t.Errorf("Get(%d) = %d", key, value)
				}
				c.Delete(key)
				c.PurgeExpired()
				if i%10 == 0 {
					c.Clear()
				}
				if n := c.Len(); n > 16 {
					t.Errorf("capacity exceeded: %d", n)
				}
			}
		})
	}
	wg.Wait()
	if c.Len() != 0 {
		t.Fatal("deleted entries remain")
	}
}

func TestCacheRejectsInvalidKeysWithoutLosingCapacity(t *testing.T) {
	t.Parallel()
	c := newCache[any, int](t, 1)
	c.Set("live", 42, 0)
	for _, tt := range []struct {
		name string
		key  any
	}{
		{"nan", math.NaN()},
		{"struct with nan", struct{ Value float64 }{math.NaN()}},
		{"non-comparable interface", []int{1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, op := range []struct {
				name string
				fn   func()
			}{
				{"Set", func() { c.Set(tt.key, 1, 0) }},
				{"Get", func() { c.Get(tt.key) }},
				{"Delete", func() { c.Delete(tt.key) }},
			} {
				t.Run(op.name, func(t *testing.T) {
					var value any
					func() {
						defer func() { value = recover() }()
						op.fn()
					}()
					if value == nil {
						t.Fatal("invalid key did not panic")
					}
					if value, ok := c.Get("live"); !ok || value != 42 || c.Len() != 1 {
						t.Fatalf("invalid key changed existing entry: value=%d, found=%v, len=%d", value, ok, c.Len())
					}
				})
			}
		})
	}
	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	if n := c.Len(); n != 1 {
		t.Fatalf("capacity after invalid keys = %d", n)
	}
	if value, ok := c.Get("b"); !ok || value != 2 {
		t.Fatalf("valid key = %d, %v", value, ok)
	}
}

func newCache[K comparable, V any](t *testing.T, capacity int) *ttlcache.Cache[K, V] {
	t.Helper()
	c, err := ttlcache.New[K, V](capacity)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ExampleCache() {
	c, err := ttlcache.New[string, string](100)
	if err != nil {
		panic(err)
	}
	c.Set("locale", "zh-CN", time.Minute)
	value, ok := c.Get("locale")
	fmt.Println(value, ok)
	// Output: zh-CN true
}
