// Package ttlcache provides an in-memory LRU cache with per-entry expiry.
package ttlcache

import (
	"container/heap"
	"container/list"
	"errors"
	"sync"
	"time"
)

// ErrInvalidCapacity reports a non-positive capacity passed to New.
var ErrInvalidCapacity = errors.New("ttlcache: capacity must be positive")

// Option configures a cache at construction time.
type Option func(*options)

type options struct {
	now func() time.Time
}

// WithClock sets the expiry clock; nil keeps time.Now. The function runs under
// the cache lock and must not reenter the cache. Clock changes do not adjust
// existing deadlines. A clock with monotonic progression is recommended.
func WithClock(now func() time.Time) Option {
	return func(o *options) {
		if now != nil {
			o.now = now
		}
	}
}

// Cache stores entries up to its configured capacity, or without a capacity
// limit when created by NewUnbounded. Get and Set make an
// entry most recently used; reads do not extend its TTL. Expiry is lazy, with no
// background goroutine. Capacity bounds entries, not the memory held by values.
// Values are shared without copying; callers must synchronize mutable contents.
// Methods are safe for concurrent use. A Cache must not be copied after first use.
// The zero value is a disabled cache: reads miss and writes are ignored.
// Keys must support equality and equal themselves. Key-taking methods panic for
// non-reflexive keys (such as NaN) or interface keys containing non-comparable values.
type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	items    map[K]*entry[K, V]
	recent   list.List
	expiry   expiryHeap[K, V]
	now      func() time.Time
}

type entry[K comparable, V any] struct {
	key       K
	value     V
	expires   time.Time
	elem      *list.Element
	heapIndex int
}

// New returns a cache with a positive entry capacity, or ErrInvalidCapacity.
func New[K comparable, V any](capacity int, opts ...Option) (*Cache[K, V], error) {
	if capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	c := NewUnbounded[K, V](opts...)
	c.capacity = capacity
	return c, nil
}

// NewUnbounded returns a cache without an entry limit. Expiry remains lazy;
// callers must bound growth or call PurgeExpired when keys are no longer read.
func NewUnbounded[K comparable, V any](opts ...Option) *Cache[K, V] {
	o := options{now: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return &Cache[K, V]{items: make(map[K]*entry[K, V]), now: o.now}
}

// Get returns the value for key and makes it most recently used. Missing or
// expired keys return the zero V and false; an expired entry is removed.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	return c.lookup(key, true, false)
}

// Peek returns a live value without refreshing its LRU position. Expired values
// are removed. Values are shared with the same ownership rules as Get.
func (c *Cache[K, V]) Peek(key K) (V, bool) {
	return c.lookup(key, false, false)
}

// LoadAndDelete atomically removes key and returns its value if still live.
// Missing or expired entries return the zero V and false. It does not dispose
// of the value or transfer exclusive ownership of references held by other users.
func (c *Cache[K, V]) LoadAndDelete(key K) (V, bool) {
	return c.lookup(key, false, true)
}

func (c *Cache[K, V]) lookup(key K, touch, remove bool) (V, bool) {
	checkKey(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if item, ok := c.items[key]; ok {
		if !expired(item.expires, c.now()) {
			if remove {
				c.remove(item)
			} else if touch {
				c.recent.MoveToFront(item.elem)
			}
			return item.value, true
		}
		c.remove(item)
	}
	var zero V
	return zero, false
}

// Set inserts or replaces a value. A positive ttl expires at now+ttl, including
// that instant; ttl <= 0 never expires. Replacing a value resets its TTL.
// A full cache evicts one expired entry if present, otherwise its least recently
// used entry. An expiry heap avoids scanning live entries. Updating, removing or
// evicting an expiring entry takes O(log n); other index operations take O(1).
func (c *Cache[K, V]) Set(key K, value V, ttl time.Duration) {
	checkKey(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		return
	}
	now := c.now()
	var expires time.Time
	if ttl > 0 {
		expires = now.Add(ttl)
	}
	if item, ok := c.items[key]; ok {
		item.value = value
		c.setExpiry(item, expires)
		c.recent.MoveToFront(item.elem)
		return
	}
	if c.capacity > 0 && len(c.items) == c.capacity {
		if len(c.expiry) > 0 && expired(c.expiry[0].expires, now) {
			c.remove(c.expiry[0])
		} else {
			c.remove(c.recent.Back().Value.(*entry[K, V]))
		}
	}
	item := &entry[K, V]{key: key, value: value, heapIndex: -1}
	item.elem = c.recent.PushFront(item)
	c.items[key] = item
	c.setExpiry(item, expires)
}

// Delete removes key, whether expired or live.
func (c *Cache[K, V]) Delete(key K) {
	checkKey(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if item, ok := c.items[key]; ok {
		c.remove(item)
	}
}

// Len removes expired entries and returns the number of live entries.
// Removing k expired entries takes O(k log n); with none expired this takes O(1).
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now != nil {
		c.purgeExpired(c.now())
	}
	return len(c.items)
}

// PurgeExpired removes expired entries and returns how many it removed.
// Removing k expired entries takes O(k log n); with none expired this takes O(1).
func (c *Cache[K, V]) PurgeExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now == nil {
		return 0
	}
	return c.purgeExpired(c.now())
}

// Clear removes every entry. It does not close or otherwise dispose of values.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.items)
	c.recent.Init()
	c.expiry = nil
}

func (c *Cache[K, V]) purgeExpired(now time.Time) int {
	removed := 0
	for len(c.expiry) > 0 && expired(c.expiry[0].expires, now) {
		c.remove(c.expiry[0])
		removed++
	}
	return removed
}

func (c *Cache[K, V]) remove(item *entry[K, V]) {
	delete(c.items, item.key)
	c.recent.Remove(item.elem)
	if item.heapIndex >= 0 {
		heap.Remove(&c.expiry, item.heapIndex)
	}
}

func (c *Cache[K, V]) setExpiry(item *entry[K, V], expires time.Time) {
	item.expires = expires
	switch {
	case expires.IsZero() && item.heapIndex >= 0:
		heap.Remove(&c.expiry, item.heapIndex)
	case expires.IsZero():
	case item.heapIndex >= 0:
		heap.Fix(&c.expiry, item.heapIndex)
	default:
		heap.Push(&c.expiry, item)
	}
}

func expired(expires, now time.Time) bool {
	return !expires.IsZero() && !now.Before(expires)
}

func checkKey[K comparable](key K) {
	if key != key { //nolint:gocritic // NaN keys cannot be retrieved or deleted from a map.
		panic("ttlcache: key must equal itself")
	}
}
