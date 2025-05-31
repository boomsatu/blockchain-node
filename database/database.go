package database

import (
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/ethdb" // Target: Go-Ethereum v1.15.11
	"github.com/syndtr/goleveldb/leveldb"
	ldb_errors "github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// Interface Database mendefinisikan operasi dasar database.
type Database interface {
	Get(key []byte) ([]byte, error)
	Put(key []byte, value []byte) error
	Delete(key []byte) error
	Close() error
	GetEthDB() ethdb.Database
}

// LevelDB adalah implementasi Database menggunakan LevelDB.
type LevelDB struct {
	db *leveldb.DB
}

// NewLevelDB membuat instance LevelDB baru.
func NewLevelDB(path string) (*LevelDB, error) {
	opts := &opt.Options{
		Filter: filter.NewBloomFilter(10),
	}
	db, err := leveldb.OpenFile(path, opts)
	if err != nil {
		if ldb_errors.IsCorrupted(err) {
			db, err = leveldb.RecoverFile(path, nil)
		}
		if err != nil {
			return nil, err
		}
	}
	return &LevelDB{db: db}, nil
}

// Get mengambil nilai berdasarkan kunci. Mengembalikan (nil, nil) jika tidak ditemukan secara internal.
func (ldb *LevelDB) Get(key []byte) ([]byte, error) {
	value, err := ldb.db.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, nil
	}
	return value, err
}

// Put menyimpan pasangan kunci-nilai.
func (ldb *LevelDB) Put(key []byte, value []byte) error {
	return ldb.db.Put(key, value, nil)
}

// Delete menghapus nilai berdasarkan kunci.
func (ldb *LevelDB) Delete(key []byte) error {
	return ldb.db.Delete(key, nil)
}

// Close menutup koneksi database.
func (ldb *LevelDB) Close() error {
	return ldb.db.Close()
}

// GetEthDB mengembalikan wrapper LevelDB yang mengimplementasikan ethdb.Database.
func (ldb *LevelDB) GetEthDB() ethdb.Database {
	return &EthDBWrapper{ldb}
}

// EthDBWrapper adalah wrapper di sekitar LevelDB untuk implementasi ethdb.Database.
type EthDBWrapper struct {
	db *LevelDB
}

// Has memeriksa apakah kunci ada di database.
func (w *EthDBWrapper) Has(key []byte) (bool, error) {
	val, err := w.db.Get(key)
	if err != nil {
		return false, err
	}
	return val != nil, nil
}

// Get mengambil nilai. Mengembalikan ethdb.ErrNotFound jika tidak ditemukan.
func (w *EthDBWrapper) Get(key []byte) ([]byte, error) {
	val, err := w.db.Get(key)
	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, ethdb.ErrNotFound // Menggunakan ethdb.ErrNotFound
	}
	return val, nil
}

// Put menyimpan nilai.
func (w *EthDBWrapper) Put(key []byte, value []byte) error {
	return w.db.Put(key, value)
}

// Delete menghapus kunci.
func (w *EthDBWrapper) Delete(key []byte) error {
	return w.db.Delete(key)
}

// DeleteRange menghapus rentang kunci.
func (w *EthDBWrapper) DeleteRange(start, end []byte) error {
	iter := w.db.db.NewIterator(&util.Range{Start: start, Limit: end}, nil)
	defer iter.Release()
	batch := new(leveldb.Batch)
	for iter.Next() {
		key := make([]byte, len(iter.Key()))
		copy(key, iter.Key())
		batch.Delete(key)
	}
	if err := iter.Error(); err != nil {
		return err
	}
	return w.db.db.Write(batch, nil)
}

// NewBatch membuat batch baru.
func (w *EthDBWrapper) NewBatch() ethdb.Batch {
	return &BatchWrapper{batch: new(leveldb.Batch), db: w.db}
}

// NewBatchWithSize membuat batch baru dengan ukuran yang diisyaratkan.
func (w *EthDBWrapper) NewBatchWithSize(size int) ethdb.Batch {
	return &BatchWrapper{batch: new(leveldb.Batch), db: w.db, intendedSize: size}
}

// SnapshotWrapper membungkus snapshot LevelDB.
type SnapshotWrapper struct {
	snap *leveldb.Snapshot
}

// Get dari snapshot. Mengembalikan ethdb.ErrNotFound jika tidak ditemukan.
func (sw *SnapshotWrapper) Get(key []byte) ([]byte, error) {
	val, err := sw.snap.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ethdb.ErrNotFound // Menggunakan ethdb.ErrNotFound
	}
	return val, err
}

// Has pada snapshot.
func (sw *SnapshotWrapper) Has(key []byte) (bool, error) {
	return sw.snap.Has(key, nil)
}

// NewIterator pada snapshot.
func (sw *SnapshotWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	var rnge *util.Range
	if prefix != nil {
		rnge = util.BytesPrefix(prefix)
		if start != nil && len(start) >= len(prefix) && strings.HasPrefix(string(start), string(prefix)) && string(start) > string(rnge.Start) {
			rnge.Start = start
		}
	} else if start != nil {
		rnge = &util.Range{Start: start}
	}
	return &IteratorWrapper{iter: sw.snap.NewIterator(rnge, nil)}
}

// Release melepaskan snapshot.
func (sw *SnapshotWrapper) Release() {
	sw.snap.Release()
}

// LDB mengembalikan objek LevelDB snapshot yang mendasarinya.
func (sw *SnapshotWrapper) LDB() interface{} {
	return sw.snap
}

// Implementasi Metered* untuk SnapshotWrapper (stub sederhana).
func (sw *SnapshotWrapper) MeteredGet(key []byte) ([]byte, error) { return sw.Get(key) }
func (sw *SnapshotWrapper) MeteredHas(key []byte) (bool, error)   { return sw.Has(key) }
func (sw *SnapshotWrapper) MeteredNewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return sw.NewIterator(prefix, start)
}

// NewSnapshot membuat snapshot database baru.
// Mengembalikan tipe ethdb.Snapshot.
func (w *EthDBWrapper) NewSnapshot() (ethdb.Snapshot, error) {
	snap, err := w.db.db.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return &SnapshotWrapper{snap: snap}, nil
}

// IteratorWrapper membungkus iterator LevelDB.
type IteratorWrapper struct {
	iter iterator.Iterator
}

func (iw *IteratorWrapper) Next() bool   { return iw.iter.Next() }
func (iw *IteratorWrapper) Error() error { return iw.iter.Error() }
func (iw *IteratorWrapper) Key() []byte {
	key := iw.iter.Key()
	if key == nil {
		return nil
	}
	copiedKey := make([]byte, len(key))
	copy(copiedKey, key)
	return copiedKey
}
func (iw *IteratorWrapper) Value() []byte {
	value := iw.iter.Value()
	if value == nil {
		return nil
	}
	copiedValue := make([]byte, len(value))
	copy(copiedValue, value)
	return copiedValue
}
func (iw *IteratorWrapper) Release() { iw.iter.Release() }

// incrementBytes adalah fungsi helper untuk membuat batas atas untuk iterasi prefix.
func incrementBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	limit := make([]byte, len(b))
	copy(limit, b)
	for i := len(limit) - 1; i >= 0; i-- {
		limit[i]++
		if limit[i] != 0 {
			return limit
		}
	}
	return nil // Overflow
}

// NewIterator membuat iterator database baru.
func (w *EthDBWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	var rnge *util.Range
	if prefix != nil {
		rnge = util.BytesPrefix(prefix)
		if start != nil && len(start) >= len(prefix) && strings.HasPrefix(string(start), string(prefix)) && string(start) > string(rnge.Start) {
			rnge.Start = start
		}
	} else if start != nil {
		rnge = &util.Range{Start: start}
	}
	return &IteratorWrapper{iter: w.db.db.NewIterator(rnge, nil)}
}

// Stat mengembalikan properti database. Sesuai dengan go-ethereum v1.15.11.
func (w *EthDBWrapper) Stat(property string) (string, error) {
	return w.db.db.GetProperty(property)
}

// Compact memadatkan rentang kunci.
func (w *EthDBWrapper) Compact(start []byte, limit []byte) error {
	var r util.Range
	if start != nil {
		r.Start = start
	}
	if limit != nil {
		r.Limit = limit
	}
	return w.db.db.CompactRange(r)
}

// Implementasi stub untuk metode AncientStore.
func (w *EthDBWrapper) AncientDatadir() (string, error) {
	return "", errors.New("EthDBWrapper: AncientDatadir not implemented")
}
func (w *EthDBWrapper) Ancients() (uint64, error) {
	return 0, errors.New("EthDBWrapper: Ancients not implemented")
}
func (w *EthDBWrapper) Ancient(kind string, number uint64) ([]byte, error) {
	return nil, errors.New("EthDBWrapper: Ancient not implemented")
}
func (w *EthDBWrapper) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	return nil, errors.New("EthDBWrapper: AncientRange not implemented")
}
func (w *EthDBWrapper) HasAncient(kind string, number uint64) (bool, error) {
	return false, errors.New("EthDBWrapper: HasAncient not implemented")
}
func (w *EthDBWrapper) AncientSize(kind string) (uint64, error) {
	return 0, errors.New("EthDBWrapper: AncientSize not implemented")
}
func (w *EthDBWrapper) ModifyAncients(callback func(ethdb.AncientWriteOp) error) (int64, error) {
	return 0, errors.New("EthDBWrapper: ModifyAncients not implemented")
}
func (w *EthDBWrapper) TruncateHead(n uint64) (uint64, error) {
	return 0, errors.New("EthDBWrapper: TruncateHead not implemented")
}
func (w *EthDBWrapper) TruncateTail(n uint64) (uint64, error) {
	return 0, errors.New("EthDBWrapper: TruncateTail not implemented")
}
func (w *EthDBWrapper) Sync() error { return errors.New("EthDBWrapper: Sync not implemented") }
func (w *EthDBWrapper) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	return errors.New("EthDBWrapper: ReadAncients not implemented")
}
func (w *EthDBWrapper) MigrateTable(name string, callback func(db ethdb.KeyValueWriter, it ethdb.Iterator) error) error {
	return errors.New("EthDBWrapper: MigrateTable not implemented")
}
func (w *EthDBWrapper) Tail() (uint64, error) {
	return 0, errors.New("EthDBWrapper: Tail not implemented")
}

// Implementasi stub untuk MeteredKeyValueStore.
func (w *EthDBWrapper) MeteredGet(key []byte) ([]byte, error)     { return w.Get(key) }
func (w *EthDBWrapper) MeteredPut(key []byte, value []byte) error { return w.Put(key, value) }
func (w *EthDBWrapper) MeteredDelete(key []byte) error            { return w.Delete(key) }
func (w *EthDBWrapper) MeteredHas(key []byte) (bool, error)       { return w.Has(key) }
func (w *EthDBWrapper) MeteredNewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return w.NewIterator(prefix, start)
}
func (w *EthDBWrapper) MeteredNewBatch() ethdb.Batch                 { return w.NewBatch() }
func (w *EthDBWrapper) MeteredNewBatchWithSize(size int) ethdb.Batch { return w.NewBatchWithSize(size) }

// SetCompactionHook menggunakan tipe ethdb.CompactionHook.
func (w *EthDBWrapper) SetCompactionHook(hook ethdb.CompactionHook) {
	// LevelDB tidak secara langsung mendukung CompactionHook melalui API ini.
	// Implementasi stub.
}
func (w *EthDBWrapper) SetThrottle(config interface{}) error {
	return errors.New("EthDBWrapper: SetThrottle not implemented")
}
func (w *EthDBWrapper) SetWriteBufferManager(manager interface{}) error {
	return errors.New("EthDBWrapper: SetWriteBufferManager not implemented")
}

// LDB mengembalikan instance LevelDB yang mendasarinya.
func (w *EthDBWrapper) LDB() interface{} { return w.db.db }

// Close menutup database.
func (w *EthDBWrapper) Close() error { return w.db.Close() }

// BatchWrapper mengimplementasikan ethdb.Batch.
type BatchWrapper struct {
	batch        *leveldb.Batch
	db           *LevelDB
	size         int
	intendedSize int
}

func (b *BatchWrapper) Put(key, value []byte) error {
	b.batch.Put(key, value)
	b.size += len(key) + len(value)
	return nil
}
func (b *BatchWrapper) Delete(key []byte) error {
	b.batch.Delete(key)
	b.size += len(key)
	return nil
}
func (b *BatchWrapper) ValueSize() int { return b.size }
func (b *BatchWrapper) Write() error   { return b.db.db.Write(b.batch, nil) }
func (b *BatchWrapper) Reset() {
	b.batch.Reset()
	b.size = 0
}
func (b *BatchWrapper) Replay(writer ethdb.KeyValueWriter) error {
	return errors.New("BatchWrapper: Replay not implemented")
}
