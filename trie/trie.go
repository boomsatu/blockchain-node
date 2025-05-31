package trie

import (
	"blockchain-node/crypto"
	"blockchain-node/database"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Node types
const (
	NodeTypeBranch    = 0
	NodeTypeExtension = 1
	NodeTypeLeaf      = 2
)

// Trie represents a Patricia Merkle Trie
type Trie struct {
	db   database.Database
	root *Node // root adalah field privat
}

// Node represents a trie node
type Node struct {
	Type     int            `json:"type"`
	Key      []byte         `json:"key,omitempty"`
	Value    []byte         `json:"value,omitempty"`
	Children map[byte]*Node `json:"children,omitempty"` // Untuk Branch dan Extension (meski Extension hanya 1)
	Hash     [32]byte       `json:"hash"`               // Hash dari node ini (setelah di-commit atau dimuat)
	Dirty    bool           `json:"-"`                  // True jika node dimodifikasi dan perlu di-commit ulang
}

// NewTrie creates a new trie
func NewTrie(rootHash [32]byte, db database.Database) (*Trie, error) {
	trie := &Trie{
		db: db,
	}

	if rootHash != ([32]byte{}) { // Jika rootHash bukan hash kosong
		node, err := trie.loadNode(rootHash)
		if err != nil {
			// Jika root node tidak ditemukan di DB, itu bisa berarti state kosong
			// atau DB korup. Untuk state kosong, root akan nil.
			if err.Error() == fmt.Sprintf("node not found: %x", rootHash) {
				// logger.Warningf("Root node %x not found in DB for trie, starting with empty trie.", rootHash)
				trie.root = nil // Mulai dengan trie kosong jika root tidak ditemukan
			} else {
				return nil, fmt.Errorf("failed to load root node %x: %v", rootHash, err)
			}
		} else {
			trie.root = node
		}
	}
	// Jika rootHash adalah hash kosong, trie.root akan tetap nil (trie kosong)
	return trie, nil
}

// RootNode mengembalikan node root dari trie.
// BARU: Method untuk mengakses root node.
func (t *Trie) RootNode() *Node {
	if t == nil {
		return nil
	}
	return t.root
}

// Get retrieves a value from the trie
func (t *Trie) Get(key []byte) ([]byte, error) {
	if t.root == nil {
		return nil, nil // Trie kosong
	}
	// Konversi key ke nibbles jika Patricia Merkle Trie Anda menggunakan nibbles
	// Untuk contoh ini, kita asumsikan key byte langsung.
	// Jika menggunakan nibbles: nibbledKey := hexToNibbles(key)
	return t.get(t.root, key, 0) // Atau nibbledKey
}

// Update inserts or updates a value in the trie
func (t *Trie) Update(key, value []byte) error {
	if len(value) == 0 {
		return t.Delete(key) // Menghapus jika value kosong
	}
	// nibbledKey := hexToNibbles(key) // Jika menggunakan nibbles
	newRoot, err := t.update(t.root, key, 0, value) // Atau nibbledKey
	if err != nil {
		return err
	}
	t.root = newRoot
	return nil
}

// Delete removes a key from the trie
func (t *Trie) Delete(key []byte) error {
	if t.root == nil {
		return nil // Tidak ada yang dihapus dari trie kosong
	}
	// nibbledKey := hexToNibbles(key) // Jika menggunakan nibbles
	newRoot, err := t.delete(t.root, key, 0) // Atau nibbledKey
	if err != nil {
		return err
	}
	t.root = newRoot
	return nil
}

// Commit commits all pending changes to the database and returns the new root hash.
func (t *Trie) Commit() ([32]byte, error) {
	if t.root == nil {
		return [32]byte{}, nil // Hash kosong untuk trie kosong
	}
	// Commit node root dan semua turunannya yang 'dirty'
	newRootHash, err := t.commitNode(t.root)
	if err != nil {
		return [32]byte{}, err
	}
	// Setelah commit, root node (dan turunannya) tidak lagi dirty dan memiliki hash yang benar.
	// t.root.Hash akan diisi oleh commitNode.
	return newRootHash, nil
}

// Copy creates a deep copy of the trie (atau shallow copy jika lebih sesuai)
// Implementasi copy bisa kompleks tergantung kebutuhan.
// Untuk stateDB, copy biasanya diperlukan untuk snapshot.
func (t *Trie) Copy() *Trie {
	newTrie := &Trie{
		db: t.db, // DB bisa di-share atau perlu mekanisme copy-on-write
	}
	if t.root != nil {
		newTrie.root = t.copyNode(t.root) // Perlu implementasi copyNode yang benar
	}
	return newTrie
}

// get, update, delete, commitNode, loadNode, copyNode, dan utility functions lainnya
// (hexToNibbles, commonPrefixLength) tetap sama seperti sebelumnya.
// Pastikan logika mereka sudah benar untuk implementasi trie Anda.

// Contoh implementasi sederhana untuk get (asumsi key byte, bukan nibbles)
func (t *Trie) get(node *Node, key []byte, depth int) ([]byte, error) {
	if node == nil {
		return nil, nil
	}

	// Logika ini sangat bergantung pada struktur node Anda (Branch, Extension, Leaf)
	// Ini adalah placeholder yang sangat disederhanakan.
	// Anda perlu menggantinya dengan logika trie yang benar.
	switch node.Type {
	case NodeTypeLeaf:
		// Jika key node leaf cocok dengan sisa key pencarian
		if bytes.Equal(node.Key, key[depth:]) { // Asumsi node.Key adalah sisa path
			return node.Value, nil
		}
		return nil, nil // Tidak cocok

	case NodeTypeExtension:
		// Jika key pencarian dimulai dengan path extension node
		if len(key) >= depth+len(node.Key) && bytes.Equal(node.Key, key[depth:depth+len(node.Key)]) {
			// Lanjutkan ke anak extension node
			// Asumsi extension node hanya punya satu anak yang relevan
			if len(node.Children) == 1 {
				var childNode *Node
				for _, child := range node.Children { // Ambil satu-satunya anak
					childNode = child
					break
				}
				// Jika anak adalah hash, muat dulu
				if childNode != nil && childNode.Hash != ([32]byte{}) && childNode.Type == 0 && childNode.Key == nil && childNode.Value == nil && childNode.Children == nil {
					loadedChild, err := t.loadNode(childNode.Hash)
					if err != nil {
						return nil, fmt.Errorf("failed to load child of extension node %x: %v", node.Hash, err)
					}
					childNode = loadedChild
				}
				return t.get(childNode, key, depth+len(node.Key))
			}
			return nil, fmt.Errorf("extension node %x has invalid children count", node.Hash)
		}
		return nil, nil // Tidak cocok

	case NodeTypeBranch:
		if depth >= len(key) { // Key habis, nilai ada di branch node?
			return node.Value, nil // Value bisa nil jika ini bukan akhir path yang valid
		}
		// Ambil nibble/byte berikutnya dari key untuk menentukan anak mana
		nextPathElement := key[depth] // Jika menggunakan byte sebagai path element
		child := node.Children[nextPathElement]
		if child == nil {
			return nil, nil // Tidak ada jalur
		}
		// Jika anak adalah hash, muat dulu
		if child.Hash != ([32]byte{}) && child.Type == 0 && child.Key == nil && child.Value == nil && child.Children == nil {
			loadedChild, err := t.loadNode(child.Hash)
			if err != nil {
				return nil, fmt.Errorf("failed to load child of branch node %x: %v", node.Hash, err)
			}
			child = loadedChild
		}
		return t.get(child, key, depth+1)

	default:
		return nil, fmt.Errorf("unknown node type: %d for node %x", node.Type, node.Hash)
	}
}

// update, delete, commitNode, loadNode, copyNode perlu implementasi yang lengkap dan benar
// untuk Patricia Merkle Trie. Implementasi di bawah ini adalah placeholder atau sangat disederhanakan.

func (t *Trie) update(node *Node, key []byte, depth int, value []byte) (*Node, error) {
	// Implementasi update yang benar untuk MPT...
	// Ini akan melibatkan pembuatan node baru (leaf, extension, branch)
	// dan menandai node yang diubah sebagai 'dirty'.
	// Ini adalah placeholder:
	if node == nil { // Buat leaf baru jika jalur tidak ada
		newNode := &Node{
			Type:  NodeTypeLeaf,
			Key:   key[depth:], // Sisa key
			Value: value,
			Dirty: true,
		}
		return newNode, nil
	}
	// Logika rekursif untuk update...
	// ...
	node.Dirty = true // Tandai node ini sebagai dirty
	return node, errors.New("trie update not fully implemented")
}

func (t *Trie) delete(node *Node, key []byte, depth int) (*Node, error) {
	// Implementasi delete yang benar untuk MPT...
	// Ini akan melibatkan penghapusan node dan potensi penggabungan/penyederhanaan struktur trie.
	// Ini adalah placeholder:
	if node == nil {
		return nil, nil // Key tidak ditemukan
	}
	// Logika rekursif untuk delete...
	// ...
	node.Dirty = true
	return node, errors.New("trie delete not fully implemented")
}

func (t *Trie) commitNode(node *Node) ([32]byte, error) {
	if node == nil {
		return [32]byte{}, nil
	}
	if !node.Dirty && node.Hash != ([32]byte{}) { // Jika tidak dirty dan sudah punya hash, gunakan hash itu
		return node.Hash, nil
	}

	// Buat node untuk serialisasi, menggantikan anak-anak dengan hash mereka
	nodeToSerialize := Node{
		Type:  node.Type,
		Key:   node.Key,   // Key untuk leaf/extension
		Value: node.Value, // Value untuk leaf/branch
	}

	if node.Type == NodeTypeBranch || node.Type == NodeTypeExtension {
		nodeToSerialize.Children = make(map[byte]*Node)
		for pathElement, childNode := range node.Children {
			childHash, err := t.commitNode(childNode) // Rekursif commit anak
			if err != nil {
				return [32]byte{}, fmt.Errorf("failed to commit child node: %v", err)
			}
			if childHash != ([32]byte{}) { // Hanya simpan referensi hash jika anak tidak nil
				nodeToSerialize.Children[pathElement] = &Node{Hash: childHash} // Simpan HASH anak, bukan node anak
			} else {
				// Jika anak menjadi nil setelah commit (misalnya, karena delete), jangan masukkan ke children
			}
		}
		// Jika setelah commit anak, branch/extension jadi kosong, perlakukan khusus
		if len(nodeToSerialize.Children) == 0 && node.Type == NodeTypeExtension {
			// Extension tanpa anak tidak valid, seharusnya disederhanakan jadi leaf atau hilang
			// Ini tergantung logika update/delete Anda. Untuk commit, ini bisa jadi error atau node kosong.
			// logger.Warningf("Committing an extension node that became empty: original hash %x", node.Hash)
			// return [32]byte{}, nil // Anggap jadi node kosong
		}
		if len(nodeToSerialize.Children) == 0 && node.Type == NodeTypeBranch && nodeToSerialize.Value == nil {
			// Branch tanpa anak dan tanpa value juga node kosong
			// logger.Warningf("Committing a branch node that became empty: original hash %x", node.Hash)
			// return [32]byte{}, nil
		}
	}

	// Serialize nodeToSerialize (yang berisi hash anak, bukan objek anak penuh)
	data, err := json.Marshal(nodeToSerialize)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to marshal node for commit: %v", err)
	}

	hash := crypto.Keccak256Hash(data)

	// Simpan node yang sudah diserialisasi (dengan hash anak) ke DB
	// Kunci DB adalah hash dari node ini.
	dbKey := append([]byte("trie_node_"), hash[:]...) // Prefix untuk membedakan dari data lain
	if err := t.db.Put(dbKey, data); err != nil {
		return [32]byte{}, fmt.Errorf("failed to store trie node %x to DB: %v", hash, err)
	}

	node.Hash = hash   // Update hash di node asli dalam memori
	node.Dirty = false // Tandai sebagai tidak dirty lagi
	// PENTING: Anak-anak dari `node` di memori masih berupa objek penuh,
	// tapi `nodeToSerialize` yang disimpan ke DB berisi hash anak.
	// Ini adalah perbedaan antara representasi memori dan persistensi.

	return hash, nil
}

func (t *Trie) loadNode(hash [32]byte) (*Node, error) {
	if hash == ([32]byte{}) {
		return nil, errors.New("cannot load node for zero hash")
	}
	dbKey := append([]byte("trie_node_"), hash[:]...)
	data, err := t.db.Get(dbKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get node %x from DB: %v", hash, err)
	}
	if data == nil {
		return nil, fmt.Errorf("node not found: %x", hash)
	}

	var loadedNode Node
	if err := json.Unmarshal(data, &loadedNode); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node %x from DB data: %v", hash, err)
	}
	loadedNode.Hash = hash // Set hash karena tidak disimpan dalam JSON value
	loadedNode.Dirty = false

	// Anak-anak yang dimuat dari `loadedNode.Children` akan berupa &Node{Hash: childHash}.
	// Mereka perlu di-resolve (dimuat dari DB) saat diakses jika diperlukan (lazy loading).
	// Atau, Anda bisa memuat semua anak secara rekursif di sini (eager loading), tapi bisa mahal.
	// Untuk sekarang, kita biarkan sebagai hash. Logika `get` atau traversal lain harus menangani ini.
	// Jika children di node yang disimpan di DB adalah hash, maka saat unmarshal,
	// field `Children` di `loadedNode` akan berisi map ke &Node{Hash: ...}.
	// Ini sudah benar untuk representasi yang disimpan.
	// Saat node ini digunakan (misal, di `get`), jika kita perlu mengakses anak,
	// kita akan melihat field Hash-nya dan memuatnya dari DB jika perlu.

	return &loadedNode, nil
}

func (t *Trie) copyNode(node *Node) *Node {
	if node == nil {
		return nil
	}
	newNode := &Node{
		Type:  node.Type,
		Key:   bytes.Clone(node.Key),
		Value: bytes.Clone(node.Value),
		Hash:  node.Hash,
		Dirty: node.Dirty, // Status dirty juga disalin
	}
	if node.Children != nil {
		newNode.Children = make(map[byte]*Node)
		for k, child := range node.Children {
			// Saat menyalin, kita menyalin referensi ke node anak (atau hash-nya jika itu yang disimpan).
			// Jika Anda ingin deep copy dari anak-anak juga, panggil copyNode rekursif.
			// Untuk snapshot stateDB, biasanya copy-on-write, jadi shallow copy dari struktur
			// dan deep copy saat modifikasi mungkin lebih efisien.
			// Untuk simplifikasi, kita lakukan shallow copy dari map children.
			// Jika child adalah pointer ke node penuh, itu disalin. Jika child adalah &Node{Hash:...}, itu juga disalin.
			newNode.Children[k] = child // Ini menyalin pointer, bukan deep copy node anak
		}
	}
	return newNode
}

// Utility functions (hexToNibbles, commonPrefixLength) bisa tetap ada jika MPT Anda menggunakannya.
// func hexToNibbles(hex []byte) []byte { ... }
// func commonPrefixLength(a, b []byte) int { ... }
