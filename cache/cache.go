package cache

import (
	"sync"
	"time"
)

// DefaultTTL adalah Time-To-Live default untuk item cache jika tidak ditentukan.
const DefaultTTL = 5 * time.Minute

// DefaultCleanupInterval adalah interval default untuk menjalankan proses cleanup.
const DefaultCleanupInterval = 1 * time.Minute

type CacheItem struct {
	Value      interface{}
	Expiration int64 // UnixNano
}

type Cache struct {
	items           map[string]*CacheItem
	mutex           sync.RWMutex
	defaultTTL      time.Duration
	cleanupInterval time.Duration
	stopCleanup     chan struct{} // Untuk menghentikan goroutine cleanup
}

// NewCache membuat instance cache baru dengan TTL default dan interval cleanup yang dapat dikonfigurasi.
func NewCache(defaultTTL time.Duration, cleanupInterval time.Duration) *Cache {
	if defaultTTL <= 0 {
		defaultTTL = DefaultTTL
	}
	if cleanupInterval <= 0 {
		cleanupInterval = DefaultCleanupInterval
	}

	cache := &Cache{
		items:           make(map[string]*CacheItem),
		defaultTTL:      defaultTTL,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}
	go cache.runCleanup()
	return cache
}

// Set menambahkan item ke cache dengan durasi tertentu.
// Jika durasi adalah 0, TTL default akan digunakan.
// Jika durasi negatif, item tidak akan pernah kedaluwarsa (tidak direkomendasikan untuk cleanup otomatis).
func (c *Cache) Set(key string, value interface{}, duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var expiration int64
	if duration == 0 {
		duration = c.defaultTTL
	}

	if duration > 0 {
		expiration = time.Now().Add(duration).UnixNano()
	}
	// Jika duration < 0, expiration akan 0 (tidak pernah kedaluwarsa oleh cleanup otomatis)

	c.items[key] = &CacheItem{
		Value:      value,
		Expiration: expiration,
	}
}

// Get mengambil item dari cache. Mengembalikan nilai dan boolean yang menunjukkan apakah item ditemukan.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mutex.RLock()
	item, exists := c.items[key]
	c.mutex.RUnlock()

	if !exists {
		return nil, false
	}

	// Periksa kedaluwarsa hanya jika expiration di-set ( > 0)
	if item.Expiration > 0 && time.Now().UnixNano() > item.Expiration {
		// Item kedaluwarsa, hapus secara atomik
		c.mutex.Lock()
		// Periksa lagi jika item masih ada dan kedaluwarsa (untuk menghindari race condition)
		if currentItem, stillExists := c.items[key]; stillExists && currentItem.Expiration == item.Expiration {
			delete(c.items, key)
		}
		c.mutex.Unlock()
		return nil, false // Kembalikan sebagai tidak ditemukan
	}

	return item.Value, true
}

// Delete menghapus item dari cache.
func (c *Cache) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.items, key)
}

// Clear menghapus semua item dari cache.
func (c *Cache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.items = make(map[string]*CacheItem)
}

// Count mengembalikan jumlah item dalam cache.
func (c *Cache) Count() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.items)
}

// runCleanup menjalankan proses cleanup periodik untuk item yang kedaluwarsa.
func (c *Cache) runCleanup() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.mutex.Lock()
			now := time.Now().UnixNano()
			for key, item := range c.items {
				if item.Expiration > 0 && now > item.Expiration {
					delete(c.items, key)
				}
			}
			c.mutex.Unlock()
		case <-c.stopCleanup: // Sinyal untuk menghentikan goroutine
			return
		}
	}
}

// StopCleanup menghentikan goroutine cleanup.
func (c *Cache) StopCleanup() {
	close(c.stopCleanup)
}
