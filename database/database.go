package database

import (
	"errors" // Impor untuk errors.New
	"strings"

	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/syndtr/goleveldb/leveldb"
	ldb_errors "github.com/syndtr/goleveldb/leveldb/errors" // Alias untuk menghindari konflik
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/iterator" // Untuk iterator.Iterator
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util" // BARU: Untuk util.Range
)

// ErrNotFound diekspor untuk digunakan oleh package lain.
var ErrNotFound = ldb_errors.ErrNotFound // Ekspor error ErrNotFound dari leveldb

type Database interface {
	Get(key []byte) ([]byte, error)
	Put(key []byte, value []byte) error
	Delete(key []byte) error
	Close() error
	GetEthDB() ethdb.Database // Metode untuk mendapatkan wrapper ethdb
}

type LevelDB struct {
	db *leveldb.DB
}

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

func (ldb *LevelDB) Get(key []byte) ([]byte, error) {
	value, err := ldb.db.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) { // Cara yang lebih baik untuk memeriksa error
		return nil, nil
	}
	return value, err
}

func (ldb *LevelDB) Put(key []byte, value []byte) error {
	return ldb.db.Put(key, value, nil)
}

func (ldb *LevelDB) Delete(key []byte) error {
	return ldb.db.Delete(key, nil)
}

func (ldb *LevelDB) Close() error {
	return ldb.db.Close()
}

func (ldb *LevelDB) GetEthDB() ethdb.Database {
	return &EthDBWrapper{ldb}
}

// EthDBWrapper membungkus LevelDB kita untuk mengimplementasikan interface ethdb.Database
type EthDBWrapper struct {
	db *LevelDB
}

func (w *EthDBWrapper) Has(key []byte) (bool, error) {
	val, err := w.db.Get(key)
	if err != nil && !errors.Is(err, leveldb.ErrNotFound) && err != nil { // Jika error BUKAN not found
		return false, err
	}
	return val != nil, nil // val akan nil jika not found (karena Get mengembalikan nil,nil)
}

func (w *EthDBWrapper) Get(key []byte) ([]byte, error) {
	val, err := w.db.Get(key)
	if err == nil && val == nil { // Jika Get mengembalikan nil, nil -> not found
		return nil, ethdb.ErrNotFound // Kembalikan error standar ethdb
	}
	return val, err
}

func (w *EthDBWrapper) Put(key []byte, value []byte) error {
	return w.db.Put(key, value)
}

func (w *EthDBWrapper) Delete(key []byte) error {
	return w.db.Delete(key)
}

func (w *EthDBWrapper) DeleteRange(start, end []byte) error {
	// LevelDB tidak memiliki DeleteRange native yang efisien.
	// Implementasi ini akan lambat untuk range besar.
	// Untuk penggunaan serius, pertimbangkan implikasi performa.
	iter := w.db.db.NewIterator(&util.Range{Start: start, Limit: end}, nil)
	defer iter.Release()
	batch := new(leveldb.Batch)
	for iter.Next() {
		// Penting untuk menyalin key karena buffer iterator bisa dipakai ulang
		key := make([]byte, len(iter.Key()))
		copy(key, iter.Key())
		batch.Delete(key)
	}
	if err := iter.Error(); err != nil {
		return err
	}
	return w.db.db.Write(batch, nil)
}

func (w *EthDBWrapper) NewBatch() ethdb.Batch {
	return &BatchWrapper{batch: new(leveldb.Batch), db: w.db}
}

func (w *EthDBWrapper) NewBatchWithSize(size int) ethdb.Batch {
	return &BatchWrapper{batch: new(leveldb.Batch), db: w.db, intendedSize: size}
}

type SnapshotWrapper struct {
	snap *leveldb.Snapshot
	// db *leveldb.DB // Tidak perlu referensi db di sini jika tidak ada metode yang membutuhkannya
}

func (sw *SnapshotWrapper) Get(key []byte) ([]byte, error) {
	val, err := sw.snap.Get(key, nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, ethdb.ErrNotFound // Gunakan ethdb.ErrNotFound
	}
	return val, err
}

func (sw *SnapshotWrapper) Has(key []byte) (bool, error) {
	return sw.snap.Has(key, nil)
}

func (sw *SnapshotWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	var rnge *util.Range // BARU: Gunakan util.Range
	if prefix != nil {
		rnge = util.BytesPrefix(prefix) // util.BytesPrefix adalah cara yang baik untuk prefix
		if start != nil && string(start) > string(rnge.Start) && strings.HasPrefix(string(start), string(prefix)) {
			rnge.Start = start
		}
	} else if start != nil {
		rnge = &util.Range{Start: start}
	}
	return &IteratorWrapper{iter: sw.snap.NewIterator(rnge, nil)}
}

func (sw *SnapshotWrapper) Release() {
	sw.snap.Release()
}

func (sw *SnapshotWrapper) LDB() interface{} {
	return sw.snap
}

// Implementasi Metered* untuk SnapshotWrapper
func (sw *SnapshotWrapper) MeteredGet(key []byte) ([]byte, error) {
	// Untuk stub, sama dengan Get. Implementasi nyata mungkin melibatkan metering.
	return sw.Get(key)
}
func (sw *SnapshotWrapper) MeteredHas(key []byte) (bool, error) {
	return sw.Has(key)
}
func (sw *SnapshotWrapper) MeteredNewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return sw.NewIterator(prefix, start)
}

func (w *EthDBWrapper) NewSnapshot() (ethdb.Snapshot, error) {
	snap, err := w.db.db.GetSnapshot()
	if err != nil {
		return nil, err
	}
	return &SnapshotWrapper{snap: snap}, nil
}

type IteratorWrapper struct {
	iter iterator.Iterator // BARU: Gunakan iterator.Iterator dari leveldb/iterator
}

func (iw *IteratorWrapper) Next() bool {
	return iw.iter.Next()
}
func (iw *IteratorWrapper) Error() error {
	return iw.iter.Error()
}
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
func (iw *IteratorWrapper) Release() {
	iw.iter.Release()
}

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
	return nil // Overflow, berarti tidak ada batas atas yang efektif dalam byte slice yang sama panjangnya
}

func (w *EthDBWrapper) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	var rnge *util.Range // BARU: Gunakan util.Range
	if prefix != nil {
		rnge = util.BytesPrefix(prefix)
		// Jika start diberikan dan berada dalam prefix, sesuaikan start dari range
		if start != nil && string(start) > string(rnge.Start) && strings.HasPrefix(string(start), string(prefix)) {
			rnge.Start = start
		}
	} else if start != nil {
		rnge = &util.Range{Start: start} // Iterasi dari start dan seterusnya jika tidak ada prefix
	}
	return &IteratorWrapper{iter: w.db.db.NewIterator(rnge, nil)}
}

func (w *EthDBWrapper) Stat(property string) (string, error) {
	return w.db.db.GetProperty(property) // Coba teruskan ke LevelDB
	// return "", errors.New("EthDBWrapper: Stat property not supported: " + property)
}

// PERBAIKAN: Signature Compact
func (w *EthDBWrapper) Compact(start []byte, limit []byte) error {
	var r util.Range
	if start != nil {
		r.Start = start
	}
	if limit != nil {
		r.Limit = limit
	}
	return w.db.db.CompactRange(r)
	// return errors.New("EthDBWrapper: Compact not fully implemented")
}

// AncientStore methods (stubs)
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
func (w *EthDBWrapper) ModifyAncients(func(ethdb.AncientWriteOp) error) (int64, error) {
	return 0, errors.New("EthDBWrapper: ModifyAncients not implemented")
}
func (w *EthDBWrapper) TruncateHead(n uint64) (uint64, error) {
	return 0, errors.New("EthDBWrapper: TruncateHead not implemented")
}
func (w *EthDBWrapper) TruncateTail(n uint64) (uint64, error) {
	return 0, errors.New("EthDBWrapper: TruncateTail not implemented")
}
func (w *EthDBWrapper) Sync() error {
	return errors.New("EthDBWrapper: Sync not implemented")
}
func (w *EthDBWrapper) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	return errors.New("EthDBWrapper: ReadAncients not implemented")
}
func (w *EthDBWrapper) MigrateTable(s string, f func([]byte) ([]byte, error)) error {
	return errors.New("EthDBWrapper: MigrateTable not implemented")
}

// MeteredKeyValueStore methods
func (w *EthDBWrapper) MeteredGet(key []byte) ([]byte, error) {
	return w.Get(key)
}
func (w *EthDBWrapper) MeteredPut(key []byte, value []byte) error {
	return w.Put(key, value)
}
func (w *EthDBWrapper) MeteredDelete(key []byte) error {
	return w.Delete(key)
}
func (w *EthDBWrapper) MeteredHas(key []byte) (bool, error) {
	return w.Has(key)
}
func (w *EthDBWrapper) MeteredNewIterator(prefix []byte, start []byte) ethdb.Iterator {
	return w.NewIterator(prefix, start)
}
func (w *EthDBWrapper) MeteredNewBatch() ethdb.Batch {
	return w.NewBatch()
}
func (w *EthDBWrapper) MeteredNewBatchWithSize(size int) ethdb.Batch {
	return w.NewBatchWithSize(size)
}

func (w *EthDBWrapper) SetCompactionHook(hook ethdb.CompactionHook) {
	// LevelDB tidak secara langsung mendukung CompactionHook melalui API ini.
}
func (w *EthDBWrapper) SetThrottle(config interface{}) error {
	return errors.New("EthDBWrapper: SetThrottle not implemented")
}
func (w *EthDBWrapper) SetWriteBufferManager(manager interface{}) error {
	return errors.New("EthDBWrapper: SetWriteBufferManager not implemented")
}
func (w *EthDBWrapper) LDB() interface{} {
	return w.db.db
}

func (w *EthDBWrapper) Close() error {
	return w.db.Close()
}

type BatchWrapper struct {
	batch        *leveldb.Batch
	db           *LevelDB
	size         int
	intendedSize int // Ukuran yang diinginkan, untuk referensi, bukan enforcement ketat
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

func (b *BatchWrapper) ValueSize() int {
	return b.size
}

func (b *BatchWrapper) Write() error {
	return b.db.db.Write(b.batch, nil)
}

func (b *BatchWrapper) Reset() {
	b.batch.Reset()
	b.size = 0
}

func (b *BatchWrapper) Replay(w ethdb.KeyValueWriter) error {
	return errors.New("BatchWrapper: Replay not implemented")
}
