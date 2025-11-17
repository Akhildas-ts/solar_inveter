// internal/service/cache.go
package service

import (
	"context"
	"sync"
	"time"
)

// CacheItem represents a cached item with expiration
type CacheItem struct {
	Value      interface{}
	Expiration int64
}

// Cache provides an efficient in-memory cache with TTL (time to live )
type Cache struct {
	mu    sync.RWMutex
	items map[string]CacheItem
	stop  chan struct{}
}

// NewCache creates a new cache instance
func NewCache(cleanupInterval time.Duration) *Cache {
	c := &Cache{
		items: make(map[string]CacheItem),
		stop:  make(chan struct{}),
	}

	// Start cleanup goroutine
	go c.cleanupLoop(cleanupInterval)

	return c
}

// Set adds an item to cache with TTL
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expiration int64
	if ttl > 0 {
		expiration = time.Now().Add(ttl).UnixNano()
	}

	c.items[key] = CacheItem{
		Value:      value,
		Expiration: expiration,
	}
}

// Get retrieves an item from cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, found := c.items[key]
	if !found {
		return nil, false
	}

	// Check expiration
	if item.Expiration > 0 && time.Now().UnixNano() > item.Expiration {
		return nil, false
	}

	return item.Value, true
}

// GetOrSet retrieves from cache or sets with loader function
func (c *Cache) GetOrSet(key string, ttl time.Duration, loader func() (interface{}, error)) (interface{}, error) {
	// Try to get from cache first
	if value, found := c.Get(key); found {
		return value, nil
	}

	// Load the value
	value, err := loader()
	if err != nil {
		return nil, err
	}

	// Set in cache
	c.Set(key, value, ttl)
	return value, nil
}

// Delete removes an item from cache
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items from cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]CacheItem)
}

// Size returns the number of items in cache
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Keys returns all cache keys
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, len(c.items))
	for k := range c.items {
		keys = append(keys, k)
	}
	return keys
}

// cleanupLoop periodically removes expired items
func (c *Cache) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stop:
			return
		}
	}
}

// cleanup removes expired items
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UnixNano()
	for key, item := range c.items {
		if item.Expiration > 0 && now > item.Expiration {
			delete(c.items, key)
		}
	}
}

// Close stops the cache cleanup goroutine
func (c *Cache) Close() {
	close(c.stop)
}

// Stats returns cache statistics
func (c *Cache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	expired := 0
	now := time.Now().UnixNano()
	for _, item := range c.items {
		if item.Expiration > 0 && now > item.Expiration {
			expired++
		}
	}

	return map[string]interface{}{
		"total_items":   len(c.items),
		"expired_items": expired,
		"active_items":  len(c.items) - expired,
	}
}

// CacheConfig holds cache configuration
type CacheConfig struct {
	StatsCache      *Cache // For statistics
	MappingCache    *Cache // For mappings
	QueryCache      *Cache // For query results
	FaultCodesCache *Cache // For fault code lookups
}

// NewCacheConfig creates all caches with appropriate TTLs
func NewCacheConfig() *CacheConfig {
	return &CacheConfig{
		StatsCache:      NewCache(1 * time.Minute),  // Cleanup every minute
		MappingCache:    NewCache(5 * time.Minute),  // Cleanup every 5 minutes
		QueryCache:      NewCache(30 * time.Second), // Cleanup every 30 seconds
		FaultCodesCache: NewCache(10 * time.Minute), // Cleanup every 10 minutes
	}
}

// CloseAll closes all caches
func (cc *CacheConfig) CloseAll() {
	cc.StatsCache.Close()
	cc.MappingCache.Close()
	cc.QueryCache.Close()
	cc.FaultCodesCache.Close()
}

// ContextCache provides context-aware caching
type ContextCache struct {
	cache *Cache
}

// NewContextCache creates a new context-aware cache
func NewContextCache(cleanupInterval time.Duration) *ContextCache {
	return &ContextCache{
		cache: NewCache(cleanupInterval),
	}
}

// GetOrLoad retrieves from cache or loads with context
func (cc *ContextCache) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader func(context.Context) (interface{}, error)) (interface{}, error) {
	// Check cache first
	if value, found := cc.cache.Get(key); found {
		return value, nil
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Load with context
	value, err := loader(ctx)
	if err != nil {
		return nil, err
	}

	// Set in cache
	cc.cache.Set(key, value, ttl)
	return value, nil
}

// Set stores a value in cache
func (cc *ContextCache) Set(key string, value interface{}, ttl time.Duration) {
	cc.cache.Set(key, value, ttl)
}

// Get retrieves a value from cache
func (cc *ContextCache) Get(key string) (interface{}, bool) {
	return cc.cache.Get(key)
}

// Delete removes a value from cache
func (cc *ContextCache) Delete(key string) {
	cc.cache.Delete(key)
}

// Clear removes all values
func (cc *ContextCache) Clear() {
	cc.cache.Clear()
}

// Close stops the cache
func (cc *ContextCache) Close() {
	cc.cache.Close()
}