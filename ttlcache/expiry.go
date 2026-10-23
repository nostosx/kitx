package ttlcache

// expiryHeap indexes only entries with deadlines. Each entry has exactly one
// heap slot, so repeated TTL changes cannot accumulate stale index records.
type expiryHeap[K comparable, V any] []*entry[K, V]

func (h expiryHeap[K, V]) Len() int { return len(h) }

func (h expiryHeap[K, V]) Less(i, j int) bool { return h[i].expires.Before(h[j].expires) }

func (h expiryHeap[K, V]) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex = i
	h[j].heapIndex = j
}

func (h *expiryHeap[K, V]) Push(x any) {
	item := x.(*entry[K, V])
	item.heapIndex = len(*h)
	*h = append(*h, item)
}

func (h *expiryHeap[K, V]) Pop() any {
	old := *h
	n := len(old) - 1
	item := old[n]
	old[n] = nil
	item.heapIndex = -1
	*h = old[:n]
	return item
}
