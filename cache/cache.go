package cache

import (
	"sync"
	"time"
)

// DefaultTTL adalah Time-To-Live default untuk item cache jika tidak ditentukan.
const DefaultTTL = 5 * time.Minute

// DefaultCleanupInterval adalah interval default untuk menjalankan proses cleanup.
const DefaultCleanupInterval = 1 * time.Minute

// CacheItem merepresentasikan sebuah item dalam cache.
type CacheItem struct {
	Value      interface{} // Nilai yang disimpan.
	Expiration int64       // Waktu kedaluwarsa dalam UnixNano. 0 berarti tidak pernah kedaluwarsa oleh cleanup otomatis.
}

// Cache adalah struktur untuk penyimpanan cache sederhana berbasis memori.
type Cache struct {
	items           map[string]*CacheItem // Penyimpanan item cache.
	mutex           sync.RWMutex          // Mutex untuk sinkronisasi akses.
	defaultTTL      time.Duration         // TTL default untuk item baru.
	cleanupInterval time.Duration         // Interval untuk menjalankan pembersihan item kedaluwarsa.
	stopCleanup     chan struct{}         // Channel untuk menghentikan goroutine cleanup.
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
	// Jalankan goroutine untuk membersihkan item yang kedaluwarsa secara periodik.
	go cache.runCleanup()
	return cache
}

// Set menambahkan item ke cache dengan durasi tertentu.
// Jika durasi adalah 0, TTL default akan digunakan.
// Jika durasi negatif, item tidak akan pernah kedaluwarsa secara otomatis oleh proses cleanup (expiration akan 0).
func (c *Cache) Set(key string, value interface{}, duration time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var expiration int64
	if duration == 0 {
		duration = c.defaultTTL
	}

	// Hanya set waktu kedaluwarsa jika durasi positif.
	// Jika durasi negatif atau nol (setelah default), expiration akan 0,
	// yang berarti tidak akan dihapus oleh cleanup otomatis.
	if duration > 0 {
		expiration = time.Now().Add(duration).UnixNano()
	}

	c.items[key] = &CacheItem{
		Value:      value,
		Expiration: expiration,
	}
}

// Get mengambil item dari cache. Mengembalikan nilai dan boolean yang menunjukkan apakah item ditemukan dan belum kedaluwarsa.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mutex.RLock()
	item, exists := c.items[key]
	c.mutex.RUnlock()

	if !exists {
		return nil, false // Item tidak ada.
	}

	// Periksa kedaluwarsa hanya jika expiration di-set ( > 0).
	// Item dengan expiration 0 dianggap tidak pernah kedaluwarsa oleh mekanisme ini.
	if item.Expiration > 0 && time.Now().UnixNano() > item.Expiration {
		// Item kedaluwarsa, hapus secara atomik untuk mencegah kondisi balapan.
		c.mutex.Lock()
		// Periksa lagi jika item masih ada dan masih item yang sama yang kedaluwarsa.
		// Ini untuk menangani kasus di mana item mungkin telah di-set ulang antara RLock dan Lock.
		if currentItem, stillExists := c.items[key]; stillExists && currentItem.Expiration == item.Expiration {
			delete(c.items, key)
		}
		c.mutex.Unlock()
		return nil, false // Kembalikan sebagai tidak ditemukan karena kedaluwarsa.
	}

	return item.Value, true // Item ditemukan dan valid.
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
				// Hanya hapus item yang memiliki waktu kedaluwarsa positif dan telah lewat.
				if item.Expiration > 0 && now > item.Expiration {
					delete(c.items, key)
				}
			}
			c.mutex.Unlock()
		case <-c.stopCleanup: // Sinyal untuk menghentikan goroutine.
			return
		}
	}
}

// StopCleanup menghentikan goroutine cleanup. Ini harus dipanggil saat cache tidak lagi digunakan
// untuk mencegah kebocoran goroutine.
func (c *Cache) StopCleanup() {
	// Pastikan channel hanya ditutup sekali.
	// Ini bisa dilakukan dengan sync.Once atau dengan memeriksa apakah channel sudah nil.
	// Untuk kesederhanaan, kita asumsikan StopCleanup hanya dipanggil sekali.
	c.mutex.Lock() // Lindungi akses ke c.stopCleanup
	if c.stopCleanup != nil {
		close(c.stopCleanup)
		c.stopCleanup = nil // Set ke nil untuk menandakan sudah ditutup
	}
	c.mutex.Unlock()
}
