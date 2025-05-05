package cache

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ResultStore is an interface for storing processing results
type ResultStore interface {
	Set(id string, result interface{}) error
	Get(id string) (string, bool)
	GetTyped(id string, dest interface{}) (bool, error)
}

// InMemoryResultStore is a simple in-memory store for processing results
type InMemoryResultStore struct {
	results map[string]string
	mutex   sync.RWMutex
}

// RedisResultStore is a Redis-backed store for processing results
type RedisResultStore struct {
	client  *redis.Client
	ttl     time.Duration
	keyBase string
	mutex   sync.RWMutex // Mutex để bảo vệ các thao tác đồng thời
}

// NewInMemoryResultStore creates a new in-memory result store
func NewInMemoryResultStore() *InMemoryResultStore {
	return &InMemoryResultStore{
		results: make(map[string]string),
	}
}

// NewRedisResultStore creates a new Redis-backed result store
func NewRedisResultStore(redisURL string, ttl time.Duration, keyBase string) (*RedisResultStore, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	// Thêm cấu hình cho kết nối Redis bền vững
	opts.MaxRetries = 5
	opts.MinRetryBackoff = 100 * time.Millisecond
	opts.MaxRetryBackoff = 2 * time.Second
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.PoolSize = 10
	opts.PoolTimeout = 4 * time.Second

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	store := &RedisResultStore{
		client:  client,
		ttl:     ttl,
		keyBase: keyBase,
	}

	// Thêm cơ chế kiểm tra kết nối định kỳ
	go store.monitorConnection(redisURL, opts)

	return store, nil
}

// monitorConnection kiểm tra kết nối Redis định kỳ
func (s *RedisResultStore) monitorConnection(redisURL string, opts *redis.Options) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.client.Ping(ctx).Err()
		cancel()

		if err != nil {
			log.Printf("Redis connection check failed: %v, attempting to reconnect...", err)
			s.mutex.Lock()
			s.client.Close()
			s.client = redis.NewClient(opts)
			s.mutex.Unlock()

			// Kiểm tra kết nối mới
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = s.client.Ping(ctx).Err()
			cancel()
			if err != nil {
				log.Printf("Failed to reconnect to Redis: %v", err)
			} else {
				log.Printf("Successfully reconnected to Redis")
			}
		}
	}
}

// Set adds a result to the in-memory store
func (s *InMemoryResultStore) Set(id string, result interface{}) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	s.results[id] = string(data)
	return nil
}

// Get retrieves a result from the in-memory store
func (s *InMemoryResultStore) Get(id string) (string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result, ok := s.results[id]
	return result, ok
}

// GetTyped retrieves and unmarshals a result from the in-memory store
func (s *InMemoryResultStore) GetTyped(id string, dest interface{}) (bool, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result, ok := s.results[id]
	if !ok {
		return false, nil
	}

	if err := json.Unmarshal([]byte(result), dest); err != nil {
		return true, err
	}

	return true, nil
}

// Set adds a result to the Redis store
func (s *RedisResultStore) Set(id string, result interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fullKey := s.keyBase + ":" + id

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Thử lại 3 lần nếu gặp lỗi kết nối
	var setErr error
	for retries := 0; retries < 3; retries++ {
		setErr = s.client.Set(ctx, fullKey, data, s.ttl).Err()
		if setErr == nil {
			return nil
		}

		// Nếu có lỗi kết nối, đợi một chút và thử lại
		if setErr != nil && (setErr.Error() == "redis: connection pool timeout" ||
			setErr.Error() == "redis: connection closed" ||
			setErr.Error() == "redis: client is closed" ||
			setErr.Error() == "context deadline exceeded") {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Lỗi khác, không cần thử lại
		break
	}

	return setErr
}

// Get retrieves a result from the Redis store
func (s *RedisResultStore) Get(id string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fullKey := s.keyBase + ":" + id

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	val, err := s.client.Get(ctx, fullKey).Result()
	if err != nil {
		return "", false
	}

	return val, true
}

// GetTyped retrieves and unmarshals a result from the Redis store
func (s *RedisResultStore) GetTyped(id string, dest interface{}) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fullKey := s.keyBase + ":" + id

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// Thử lại 3 lần nếu gặp lỗi kết nối
	var val string
	var err error

	for retries := 0; retries < 3; retries++ {
		val, err = s.client.Get(ctx, fullKey).Result()
		if err == nil {
			break
		}

		if err == redis.Nil {
			return false, nil
		}

		// Nếu có lỗi kết nối, đợi một chút và thử lại
		if err != nil && (err.Error() == "redis: connection pool timeout" ||
			err.Error() == "redis: connection closed" ||
			err.Error() == "redis: client is closed" ||
			err.Error() == "context deadline exceeded") {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Lỗi khác, không cần thử lại
		return false, err
	}

	if err == redis.Nil {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return true, err
	}

	return true, nil
}
