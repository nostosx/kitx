// Package syncmap provides a generic map with synchronized access and snapshots.
package syncmap

import (
	"maps"
	"slices"
	"sync"
)

// Map is a map guarded by a read-write mutex. The zero value is ready to
// use. Map does not copy values; callers that hand out mutable values clone
// them on the way in and out. Its methods are safe for concurrent use. A Map
// must not be copied after first use. Operations on dynamic keys that are not
// comparable panic, as with a built-in map.
type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// Get returns the value for k.
func (m *Map[K, V]) Get(k K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.m[k]
	return v, ok
}

// Set stores v under k.
func (m *Map[K, V]) Set(k K, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.m == nil {
		m.m = map[K]V{}
	}
	m.m[k] = v
}

// Delete removes k and reports whether it was present.
func (m *Map[K, V]) Delete(k K) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.m[k]
	delete(m.m, k)
	return ok
}

// Values returns a shallow snapshot in unspecified order. Modifying the returned
// slice does not change the map; referenced data within its values is shared.
func (m *Map[K, V]) Values() []V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Collect(maps.Values(m.m))
}
