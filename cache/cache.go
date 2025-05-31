package cache

import (
	"sync"
	"time"
)

// DefaultTTL adalah Time-To-Live default untuk item cache.
const DefaultTTL = 5 * time.Minute // Contoh: 5 menit

type CacheItem struct {
	Value      interface{}
	Expiration int64
}

type Cache struct {
	items map[string]*CacheItem
	mutex sync.RWMutex
}

func NewCache() *Cache {
	cache := &Cache{
		items: make(map[string]*CacheItem),
	}
	go cache.cleanup()
	return cache
}

func (c *Cache) Set(key string, value interface{}, duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var expiration int64
	if duration > 0 {
		expiration = time.Now().Add(duration).UnixNano()
	}
	// Jika duration 0 atau negatif, item tidak akan pernah kedaluwarsa secara otomatis oleh cleanup (perlu dipertimbangkan)
	// Untuk DefaultTTL, kita akan selalu set expiration.

	c.items[key] = &CacheItem{
		Value:      value,
		Expiration: expiration,
	}
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mutex.RLock()
	item, exists := c.items[key]
	c.mutex.RUnlock() // Lepas read lock sebelum potensi delete

	if !exists {
		return nil, false
	}

	if item.Expiration > 0 && time.Now().UnixNano() > item.Expiration {
		// Item kedaluwarsa, hapus dari cache
		c.mutex.Lock()
		// Periksa lagi jika item masih sama (untuk menghindari race condition dengan Set)
		if currentItem, stillExists := c.items[key]; stillExists && currentItem.Expiration == item.Expiration {
			delete(c.items, key)
		}
		c.mutex.Unlock()
		return nil, false // Kembalikan sebagai tidak ditemukan
	}

	return item.Value, true
}

func (c *Cache) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.items, key)
}

func (c *Cache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.items = make(map[string]*CacheItem)
}

func (c *Cache) Count() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.items)
}

func (c *Cache) cleanup() {
	// Pemeriksaan periodik untuk item yang kedaluwarsa
	// Frekuensi cleanup bisa disesuaikan
	ticker := time.NewTicker(1 * time.Minute) // Cek setiap menit
	defer ticker.Stop()

	for range ticker.C {
		c.mutex.Lock()
		now := time.Now().UnixNano()
		for key, item := range c.items {
			if item.Expiration > 0 && now > item.Expiration {
				delete(c.items, key)
			}
		}
		c.mutex.Unlock()
	}
}
