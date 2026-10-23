package ttlcache_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestPeekPreservesRecency(t *testing.T) {
	t.Parallel()
	c := newCache[string, int](t, 2)
	c.Set("a", 1, 0)
	c.Set("b", 2, 0)
	if value, ok := c.Peek("a"); !ok || value != 1 {
		t.Fatal(value, ok)
	}
	c.Set("c", 3, 0)
	if _, ok := c.Get("a"); ok {
		t.Fatal("Peek refreshed recency")
	}
}

func TestLoadAndDeleteIsAtomic(t *testing.T) {
	t.Parallel()
	c := newCache[string, int](t, 1)
	c.Set("key", 42, 0)
	var found atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if value, ok := c.LoadAndDelete("key"); ok {
				found.Add(1)
				if value != 42 {
					t.Error(value)
				}
			}
		})
	}
	wg.Wait()
	if found.Load() != 1 || c.Len() != 0 {
		t.Fatal("removal was not atomic")
	}
}

func TestPeekAndLoadAndDeleteExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCache[string, int](t, 2)
		c.Set("peek", 1, time.Second)
		c.Set("delete", 2, time.Second)
		time.Sleep(time.Second)
		if value, ok := c.Peek("peek"); ok || value != 0 {
			t.Fatal(value, ok)
		}
		if value, ok := c.LoadAndDelete("delete"); ok || value != 0 {
			t.Fatal(value, ok)
		}
		if c.Len() != 0 {
			t.Fatal(c.Len())
		}
	})
}
