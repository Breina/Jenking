package cache

import (
	"sync"
	"time"
)

// Entry holds a cached value and the time it was stored.
type Entry[V any] struct {
	Value     V
	FetchedAt time.Time
}

// Cache is a generic, mutex-protected, LRU-evicting cache.
type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	entries  map[K]*Entry[V]
	order    []K // LRU: oldest first
	maxItems int // 0 = no eviction
}

// New creates a Cache with the given maximum capacity.
// Pass 0 for unlimited entries (no eviction).
func New[K comparable, V any](maxItems int) *Cache[K, V] {
	return &Cache[K, V]{
		entries:  make(map[K]*Entry[V]),
		maxItems: maxItems,
	}
}

// Get returns the cached entry for key, or nil if absent.
func (c *Cache[K, V]) Get(key K) *Entry[V] {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil
	}
	c.touchLocked(key)
	return e
}

// Put stores a value, evicting the oldest entry if at capacity.
func (c *Cache[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		c.entries[key] = &Entry[V]{Value: value, FetchedAt: time.Now()}
		c.touchLocked(key)
		return
	}
	if c.maxItems > 0 && len(c.entries) >= c.maxItems {
		c.evictOldestLocked()
	}
	c.entries[key] = &Entry[V]{Value: value, FetchedAt: time.Now()}
	c.order = append(c.order, key)
}

// Delete removes an entry.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
	c.removeFromOrder(key)
}

// Snapshot returns a copy of all key/value pairs currently in the cache. The
// returned map is detached — callers may mutate it freely.
func (c *Cache[K, V]) Snapshot() map[K]V {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[K]V, len(c.entries))
	for k, e := range c.entries {
		out[k] = e.Value
	}
	return out
}

// Size returns the number of entries currently in the cache.
func (c *Cache[K, V]) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Age returns the time since the entry was stored, or -1 if absent.
func (c *Cache[K, V]) Age(key K) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return -1
	}
	return time.Since(e.FetchedAt)
}

// touchLocked moves key to the end of the LRU order (most recently used).
func (c *Cache[K, V]) touchLocked(key K) {
	c.removeFromOrder(key)
	c.order = append(c.order, key)
}

func (c *Cache[K, V]) removeFromOrder(key K) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *Cache[K, V]) evictOldestLocked() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}
