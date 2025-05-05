package cache

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"sync"
	"time"
)

// ImageTextCacheItem represents a cached OCR result
type ImageTextCacheItem struct {
	Text      string    // OCR result text
	CreatedAt time.Time // Time when cache was created
}

// Cache is a simple in-memory cache for OCR results
type Cache struct {
	items map[string]ImageTextCacheItem
	mutex sync.RWMutex
	ttl   time.Duration // Time to live for cache items
}

// NewCache creates a new cache with specified TTL
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		items: make(map[string]ImageTextCacheItem),
		ttl:   ttl,
	}
}

// GetImageHash generates a hash for an image file
func GetImageHash(imagePath string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Get retrieves an item from the cache
func (c *Cache) Get(key string) (string, bool) {
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

	return item.Text, true
}

// Set adds an item to the cache
func (c *Cache) Set(key string, text string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items[key] = ImageTextCacheItem{
		Text:      text,
		CreatedAt: time.Now(),
	}
}

// Clear empties the cache
func (c *Cache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items = make(map[string]ImageTextCacheItem)
}

// Size returns the number of items in the cache
func (c *Cache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.items)
}
