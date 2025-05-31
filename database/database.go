package database

import (
	"bytes" // Ditambahkan untuk bytes.Compare
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util" // Masih dibutuhkan untuk util.Range
)

// ErrNotFound adalah error yang dikembalikan ketika sebuah key tidak ditemukan di database.
var ErrNotFound = errors.New("database: not found")

// Snapshot adalah antarmuka untuk snapshot database yang read-only.
type Snapshot interface {
	Get(key []byte) ([]byte, error)
	Has(key []byte) (bool, error)
	Release()
	NewIterator(slice *util.Range, ro *opt.ReadOptions) Iterator
}

// Iterator adalah antarmuka untuk iterator key-value.
type Iterator interface {
	Next() bool
	Prev() bool
	First() bool
	Last() bool
	Seek(key []byte) bool
	Key() []byte
	Value() []byte
	Release()
	Error() error
}

// Batch adalah antarmuka untuk operasi tulis batch.
type Batch interface {
	Put(key, value []byte)
	Delete(key []byte)
	Write() error
	ValueSize() int
	Reset()
	Replay(w KeyValueWriter) error
}

// KeyValueWriter adalah antarmuka untuk menulis key-value.
type KeyValueWriter interface {
	Put(key []byte, value []byte) error
	Delete(key []byte) error
}

// CompactionHook adalah antarmuka untuk hook yang dijalankan selama proses compact.
type CompactionHook interface {
	Run(compactedBytes int)
}

// Database adalah antarmuka generik untuk database key-value.
type Database interface {
	Put(key []byte, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
	Has(key []byte) (bool, error)
	NewSnapshot() (Snapshot, error)
	NewIterator(slice *util.Range, ro *opt.ReadOptions) Iterator
	NewBatch() Batch
	Stat() (string, error)
	Compact(start, limit []byte) error
	Close() error
}

// EthDBWrapper adalah implementasi dari antarmuka Database menggunakan LevelDB.
type EthDBWrapper struct {
	db         *leveldb.DB
	path       string
	mu         sync.RWMutex
	compHook   CompactionHook
	quitCompac chan chan error
}

// NewLevelDB membuat atau membuka database LevelDB di path yang diberikan.
func NewLevelDB(path string) (Database, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	o := &opt.Options{ErrorIfMissing: false}
	db, err := leveldb.OpenFile(path, o)
	if err != nil {
		return nil, fmt.Errorf("failed to open leveldb at %s: %w", path, err)
	}
	return &EthDBWrapper{db: db, path: path}, nil
}

func (w *EthDBWrapper) Put(key []byte, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.db.Put(key, value, nil)
}

func (w *EthDBWrapper) Get(key []byte) ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	value, err := w.db.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ErrNotFound
	}
	return value, err
}

func (w *EthDBWrapper) Delete(key []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.db.Delete(key, nil)
}

func (w *EthDBWrapper) Has(key []byte) (bool, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.db.Has(key, nil)
}

type snapshotWrapper struct {
	snap *leveldb.Snapshot
}

func (sw *snapshotWrapper) Get(key []byte) ([]byte, error) {
	val, err := sw.snap.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ErrNotFound
	}
	return val, err
}
func (sw *snapshotWrapper) Has(key []byte) (bool, error) { return sw.snap.Has(key, nil) }
func (sw *snapshotWrapper) Release()                     { sw.snap.Release() }
func (sw *snapshotWrapper) NewIterator(slice *util.Range, ro *opt.ReadOptions) Iterator {
	return sw.snap.NewIterator(slice, ro)
}

func (w *EthDBWrapper) NewSnapshot() (Snapshot, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	snap, err := w.db.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return &snapshotWrapper{snap: snap}, nil
}

func (w *EthDBWrapper) NewIterator(slice *util.Range, ro *opt.ReadOptions) Iterator {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.db.NewIterator(slice, ro)
}

type levelDBBatch struct {
	batch *leveldb.Batch
	db    *EthDBWrapper
}

func (w *EthDBWrapper) NewBatch() Batch {
	return &levelDBBatch{batch: new(leveldb.Batch), db: w}
}

func (b *levelDBBatch) Put(key, value []byte) { b.batch.Put(key, value) }
func (b *levelDBBatch) Delete(key []byte)     { b.batch.Delete(key) }
func (b *levelDBBatch) Write() error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	return b.db.db.Write(b.batch, nil)
}
func (b *levelDBBatch) ValueSize() int { return b.batch.Len() }
func (b *levelDBBatch) Reset()         { b.batch.Reset() }

// keyValueWriterReplayer adalah adapter untuk menggunakan database.KeyValueWriter
// dengan metode leveldb.Batch.Replay.
type keyValueWriterReplayer struct {
	writer KeyValueWriter
	err    error // Untuk menangkap error pertama dari operasi writer
}

func (r *keyValueWriterReplayer) Put(key, value []byte) {
	if r.err != nil {
		return // Jangan lakukan operasi lebih lanjut jika sudah ada error
	}
	r.err = r.writer.Put(key, value)
}

func (r *keyValueWriterReplayer) Delete(key []byte) {
	if r.err != nil {
		return
	}
	r.err = r.writer.Delete(key)
}

// Replay menjalankan kembali operasi batch pada writer yang diberikan.
// Diperbaiki untuk menggunakan b.batch.Replay()
func (b *levelDBBatch) Replay(writer KeyValueWriter) error {
	replayer := &keyValueWriterReplayer{writer: writer}
	// Metode Replay pada leveldb.Batch akan memanggil Put/Delete pada replayer.
	// Error yang dikembalikan oleh b.batch.Replay adalah error dari proses replay itu sendiri (jarang terjadi).
	if err := b.batch.Replay(replayer); err != nil {
		return fmt.Errorf("leveldb batch replay failed: %w", err)
	}
	// Kembalikan error yang mungkin terjadi selama operasi Put/Delete pada KeyValueWriter.
	return replayer.err
}

func (w *EthDBWrapper) Stat() (string, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	stats, err := w.db.GetProperty("leveldb.stats")
	if err != nil {
		return "", fmt.Errorf("failed to get leveldb stats: %w", err)
	}
	return stats, nil
}

func (w *EthDBWrapper) Compact(start, limit []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.db.CompactRange(util.Range{Start: start, Limit: limit})
}

func (w *EthDBWrapper) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.quitCompac != nil {
		errCh := make(chan error)
		w.quitCompac <- errCh
		if err := <-errCh; err != nil {
			fmt.Fprintf(os.Stderr, "Error stopping auto-compaction: %v\n", err)
		}
		w.quitCompac = nil
	}
	return w.db.Close()
}

func (w *EthDBWrapper) SetCompactionHook(hook CompactionHook) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.compHook = hook
}

func (w *EthDBWrapper) StartAutoCompact() {
	w.mu.Lock()
	if w.quitCompac != nil {
		w.mu.Unlock()
		fmt.Println("Auto-compaction already running.")
		return
	}
	w.quitCompac = make(chan chan error)
	w.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "Auto-compaction goroutine panicked: %v\n", r)
			}
		}()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		fmt.Println("Auto-compaction goroutine started.")
		for {
			select {
			case errCh := <-w.quitCompac:
				fmt.Println("Auto-compaction goroutine received stop signal.")
				errCh <- nil
				return
			case <-ticker.C:
				fmt.Println("Starting automatic database compaction...")
				if err := w.Compact(nil, nil); err != nil {
					fmt.Fprintf(os.Stderr, "Auto-compaction error: %v\n", err)
				} else {
					fmt.Println("Automatic database compaction finished.")
					w.mu.RLock()
					currentHook := w.compHook
					w.mu.RUnlock()
					if currentHook != nil {
						currentHook.Run(0)
					}
				}
			}
		}
	}()
}

// --- Implementasi MemDB ---
type MemDB struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func NewMemDB() (Database, error) {
	return &MemDB{data: make(map[string][]byte)}, nil
}

func (m *MemDB) Put(key []byte, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	valCopy := make([]byte, len(value))
	copy(valCopy, value)
	m.data[string(key)] = valCopy
	return nil
}

func (m *MemDB) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if value, ok := m.data[string(key)]; ok {
		valCopy := make([]byte, len(value))
		copy(valCopy, value)
		return valCopy, nil
	}
	return nil, ErrNotFound
}

func (m *MemDB) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}

func (m *MemDB) Has(key []byte) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[string(key)]
	return ok, nil
}

type memSnapshotView struct {
	dataCopy map[string][]byte
}

func (m *MemDB) NewSnapshot() (Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dc := make(map[string][]byte, len(m.data))
	for k, v := range m.data {
		vCopy := make([]byte, len(v))
		copy(vCopy, v)
		dc[k] = vCopy
	}
	return &memSnapshotView{dataCopy: dc}, nil
}

func (ms *memSnapshotView) Get(key []byte) ([]byte, error) {
	if val, ok := ms.dataCopy[string(key)]; ok {
		valCopy := make([]byte, len(val))
		copy(valCopy, val)
		return valCopy, nil
	}
	return nil, ErrNotFound
}
func (ms *memSnapshotView) Has(key []byte) (bool, error) {
	_, ok := ms.dataCopy[string(key)]
	return ok, nil
}
func (ms *memSnapshotView) Release() {}
func (ms *memSnapshotView) NewIterator(slice *util.Range, ro *opt.ReadOptions) Iterator {
	keys := make([]string, 0, len(ms.dataCopy))
	for k := range ms.dataCopy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return &memIterator{data: ms.dataCopy, keys: keys, current: -1, slice: slice}
}

func (m *MemDB) NewIterator(slice *util.Range, ro *opt.ReadOptions) Iterator {
	m.mu.RLock()
	keys := make([]string, 0, len(m.data))
	dataCopy := make(map[string][]byte, len(m.data))
	for k, v := range m.data {
		keys = append(keys, k)
		vCopy := make([]byte, len(v))
		copy(vCopy, v)
		dataCopy[k] = vCopy
	}
	m.mu.RUnlock()
	sort.Strings(keys)
	return &memIterator{data: dataCopy, keys: keys, current: -1, slice: slice}
}

type memIterator struct {
	data    map[string][]byte
	keys    []string
	current int
	slice   *util.Range
	lastErr error
}

func (it *memIterator) Next() bool {
	for {
		it.current++
		if it.current >= len(it.keys) {
			return false
		}
		keyBytes := []byte(it.keys[it.current])
		if it.slice != nil {
			if it.slice.Start != nil && bytes.Compare(keyBytes, it.slice.Start) < 0 { // Diperbaiki
				continue
			}
			if it.slice.Limit != nil && bytes.Compare(keyBytes, it.slice.Limit) >= 0 { // Diperbaiki
				return false
			}
		}
		return true
	}
}
func (it *memIterator) Prev() bool {
	for {
		it.current--
		if it.current < 0 {
			return false
		}
		keyBytes := []byte(it.keys[it.current])
		if it.slice != nil {
			if it.slice.Limit != nil && bytes.Compare(keyBytes, it.slice.Limit) >= 0 { // Diperbaiki
				continue
			}
			if it.slice.Start != nil && bytes.Compare(keyBytes, it.slice.Start) < 0 { // Diperbaiki
				return false
			}
		}
		return true
	}
}
func (it *memIterator) First() bool {
	it.current = -1
	return it.Next()
}
func (it *memIterator) Last() bool {
	it.current = len(it.keys)
	return it.Prev()
}
func (it *memIterator) Seek(key []byte) bool {
	idx := sort.SearchStrings(it.keys, string(key))
	it.current = idx - 1
	return it.Next()
}
func (it *memIterator) Key() []byte {
	if it.current < 0 || it.current >= len(it.keys) {
		return nil
	}
	return []byte(it.keys[it.current])
}
func (it *memIterator) Value() []byte {
	if it.current < 0 || it.current >= len(it.keys) {
		return nil
	}
	val, _ := it.data[it.keys[it.current]]
	return val
}
func (it *memIterator) Release() {
	it.keys = nil
	it.data = nil
}
func (it *memIterator) Error() error { return it.lastErr }

type memBatch struct {
	db   *MemDB
	ops  []batchOp
	size int
}
type batchOp struct {
	isPut bool
	key   []byte
	value []byte
}

func (m *MemDB) NewBatch() Batch {
	return &memBatch{db: m, ops: make([]batchOp, 0)}
}
func (b *memBatch) Put(key, value []byte) {
	k := make([]byte, len(key))
	copy(k, key)
	v := make([]byte, len(value))
	copy(v, value)
	b.ops = append(b.ops, batchOp{isPut: true, key: k, value: v})
	b.size += len(v)
}
func (b *memBatch) Delete(key []byte) {
	k := make([]byte, len(key))
	copy(k, key)
	b.ops = append(b.ops, batchOp{isPut: false, key: k})
}
func (b *memBatch) Write() error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	for _, op := range b.ops {
		if op.isPut {
			valCopy := make([]byte, len(op.value))
			copy(valCopy, op.value)
			b.db.data[string(op.key)] = valCopy
		} else {
			delete(b.db.data, string(op.key))
		}
	}
	b.Reset()
	return nil
}
func (b *memBatch) ValueSize() int { return b.size }
func (b *memBatch) Reset()         { b.ops = make([]batchOp, 0); b.size = 0 }
func (b *memBatch) Replay(w KeyValueWriter) error {
	for _, op := range b.ops {
		if op.isPut {
			if err := w.Put(op.key, op.value); err != nil {
				return err
			}
		} else {
			if err := w.Delete(op.key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *MemDB) Stat() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fmt.Sprintf("MemDB: %d items", len(m.data)), nil
}
func (m *MemDB) Compact(start, limit []byte) error { return nil }
func (m *MemDB) Close() error                      { return nil }

var _ Database = (*EthDBWrapper)(nil)
var _ Snapshot = (*snapshotWrapper)(nil)
var _ Batch = (*levelDBBatch)(nil)
var _ Iterator = (iterator.Iterator)(nil)
var _ Database = (*MemDB)(nil)
var _ Snapshot = (*memSnapshotView)(nil)
var _ Batch = (*memBatch)(nil)
var _ Iterator = (*memIterator)(nil)
