package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// TranslationCacheItem represents a cached translation result
type TranslationCacheItem struct {
	TranslatedText string    // Translated text
	CreatedAt      time.Time // Time when cache was created
}

// TranslationCache is a cache for translation results
type TranslationCache struct {
	items map[string]TranslationCacheItem
	mutex sync.RWMutex
	ttl   time.Duration // Time to live for cache items
}

// NewTranslationCache creates a new translation cache with specified TTL
func NewTranslationCache(ttl time.Duration) *TranslationCache {
	return &TranslationCache{
		items: make(map[string]TranslationCacheItem),
		ttl:   ttl,
	}
}

// GetTextHash generates a hash for text to use as cache key
func GetTextHash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

// Get retrieves a translation from the cache
func (c *TranslationCache) Get(key string) (string, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	item, exists := c.items[key]
	if !exists {
		return "", false
	}

	// Check if item has expired
	if time.Since(item.CreatedAt) > c.ttl {
		delete(c.items, key)
		return "", false
	}

	return item.TranslatedText, true
}

// Set adds a translation to the cache
func (c *TranslationCache) Set(key string, translatedText string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items[key] = TranslationCacheItem{
		TranslatedText: translatedText,
		CreatedAt:      time.Now(),
	}
}

// Clear empties the cache
func (c *TranslationCache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items = make(map[string]TranslationCacheItem)
}

// Size returns the number of items in the cache
func (c *TranslationCache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.items)
}
