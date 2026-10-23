package ttlcache

import (
	"testing"
	"time"
)

// Updating a deadline must replace its heap slot, rather than retaining stale
// expiry records that can evict the replacement or grow without bound.
func TestExpiryIndexTracksReplacementAndRemoval(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c, err := New[string, int](4, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1000 {
		c.Set("updated", i, time.Duration(i+1)*time.Second)
	}
	if len(c.expiry) != 1 {
		t.Fatalf("replacement accumulated %d expiry records", len(c.expiry))
	}
	c.Set("early", 1, time.Hour)
	c.Set("early", 2, time.Second)
	c.Set("removed", 3, time.Second)
	c.Delete("removed")
	c.Set("persistent", 4, time.Second)
	c.Set("persistent", 5, 0)
	now = now.Add(time.Second)
	if n := c.PurgeExpired(); n != 1 {
		t.Fatalf("purged %d entries, want only the shortened deadline", n)
	}
	if v, ok := c.Get("updated"); !ok || v != 999 {
		t.Fatalf("replacement lost to stale deadline: %d, %v", v, ok)
	}
	if v, ok := c.Get("persistent"); !ok || v != 5 {
		t.Fatalf("cleared TTL still expires: %d, %v", v, ok)
	}
	c.LoadAndDelete("updated")
	if len(c.expiry) != 0 {
		t.Fatal("removed entry retained in expiry index")
	}
	c.Set("clear", 6, time.Second)
	c.Clear()
	if len(c.expiry) != 0 || c.Len() != 0 {
		t.Fatal("Clear retained entries or deadlines")
	}
	c.Set("reused", 7, time.Second)
	now = now.Add(time.Second)
	if n := c.PurgeExpired(); n != 1 || c.Len() != 0 {
		t.Fatalf("reuse after Clear: purged=%d, len=%d", n, c.Len())
	}
}
