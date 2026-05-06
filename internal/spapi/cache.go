package spapi

import (
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	value     any
	expiresAt time.Time
}

type TTLCache struct {
	mu    sync.Mutex
	store map[string]cacheEntry
	ttl   time.Duration
}

func NewTTLCache(ttlSeconds int) *TTLCache {
	return &TTLCache{
		store: make(map[string]cacheEntry),
		ttl:   time.Duration(ttlSeconds) * time.Second,
	}
}

func (c *TTLCache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.store[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.store, key)
		return nil, false
	}
	return entry.value, true
}

func (c *TTLCache) Set(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *TTLCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *TTLCache) InvalidatePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.store {
		if strings.HasPrefix(k, prefix) {
			delete(c.store, k)
		}
	}
}

func (c *TTLCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]cacheEntry)
}
