package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ImageTextCacheItem represents a cached OCR result
type ImageTextCacheItem struct {
	Text      string    `json:"text"`      // OCR result text
	CreatedAt time.Time `json:"createdAt"` // Time when cache was created
}

// Cache is an interface for caching systems
type Cache interface {
	Get(key string) (string, bool)
	Set(key string, text string) error
	Clear() error
	Size() (int, error)
}

// InMemoryCache is a simple in-memory cache for OCR results
type InMemoryCache struct {
	items map[string]ImageTextCacheItem
	mutex sync.RWMutex
	ttl   time.Duration // Time to live for cache items
}

// RedisCache is a Redis-backed cache for OCR results
type RedisCache struct {
	client  *redis.Client
	ttl     time.Duration
	keyBase string
}

// NewInMemoryCache creates a new in-memory cache with specified TTL
func NewInMemoryCache(ttl time.Duration) *InMemoryCache {
	return &InMemoryCache{
		items: make(map[string]ImageTextCacheItem),
		ttl:   ttl,
	}
}

// NewRedisCache creates a new Redis-backed cache
func NewRedisCache(redisURL string, ttl time.Duration, keyBase string) (*RedisCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisCache{
		client:  client,
		ttl:     ttl,
		keyBase: keyBase,
	}, nil
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

// Get retrieves an item from the in-memory cache
func (c *InMemoryCache) Get(key string) (string, bool) {
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

// Set adds an item to the in-memory cache
func (c *InMemoryCache) Set(key string, text string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items[key] = ImageTextCacheItem{
		Text:      text,
		CreatedAt: time.Now(),
	}
	return nil
}

// Clear empties the in-memory cache
func (c *InMemoryCache) Clear() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items = make(map[string]ImageTextCacheItem)
	return nil
}

// Size returns the number of items in the in-memory cache
func (c *InMemoryCache) Size() (int, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.items), nil
}

// Get retrieves an item from the Redis cache
func (c *RedisCache) Get(key string) (string, bool) {
	ctx := context.Background()
	fullKey := c.keyBase + ":" + key

	val, err := c.client.Get(ctx, fullKey).Result()
	if err != nil {
		return "", false
	}

	var item ImageTextCacheItem
	if err := json.Unmarshal([]byte(val), &item); err != nil {
		return "", false
	}

	return item.Text, true
}

// Set adds an item to the Redis cache
func (c *RedisCache) Set(key string, text string) error {
	ctx := context.Background()
	fullKey := c.keyBase + ":" + key

	item := ImageTextCacheItem{
		Text:      text,
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(item)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, fullKey, data, c.ttl).Err()
}

// Clear empties the Redis cache for the current key base
func (c *RedisCache) Clear() error {
	ctx := context.Background()
	pattern := c.keyBase + ":*"

	keys, err := c.client.Keys(ctx, pattern).Result()
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		return c.client.Del(ctx, keys...).Err()
	}

	return nil
}

// Size returns the number of items in the Redis cache for the current key base
func (c *RedisCache) Size() (int, error) {
	ctx := context.Background()
	pattern := c.keyBase + ":*"

	keys, err := c.client.Keys(ctx, pattern).Result()
	if err != nil {
		return 0, err
	}

	return len(keys), nil
}
