package syncmap_test

import (
	"sync"
	"testing"

	"github.com/nostosx/kitx/syncmap"
)

func TestMapConcurrentAccess(t *testing.T) {
	t.Parallel()
	var m syncmap.Map[int, int]
	if _, ok := m.Get(0); ok || m.Delete(0) {
		t.Fatal("zero map contains key")
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			m.Set(i, i)
			if got, ok := m.Get(i); !ok || got != i {
				t.Errorf("Get(%d) = %d, %v", i, got, ok)
			}
			m.Values()
		})
	}
	wg.Wait()
	values := m.Values()
	if len(values) != 32 {
		t.Fatalf("lost concurrent values: %v", values)
	}
	values[0] = -1
	for i := range 32 {
		if got, ok := m.Get(i); !ok || got != i {
			t.Errorf("returned Values aliases map: %d = %d", i, got)
		}
		if !m.Delete(i) {
			t.Errorf("Delete(%d) failed", i)
		}
	}
}

func TestMapDeleteHasOneWinner(t *testing.T) {
	t.Parallel()
	var m syncmap.Map[string, int]
	m.Set("key", 1)
	m.Set("key", 2)
	if got, ok := m.Get("key"); !ok || got != 2 {
		t.Fatalf("overwrite = %d, %v", got, ok)
	}
	start := make(chan struct{})
	results := make(chan bool, 32)
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			<-start
			results <- m.Delete("key")
		})
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	for removed := range results {
		if removed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("delete winners = %d, want 1", winners)
	}
	if value, ok := m.Get("key"); ok || value != 0 {
		t.Fatalf("deleted key = %d, %v", value, ok)
	}
}
