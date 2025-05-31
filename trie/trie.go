
package trie

import (
	"blockchain-node/crypto"
	"blockchain-node/database"
	"bytes"
	"encoding/json"
	"fmt"
)

// Node types
const (
	NodeTypeBranch   = 0
	NodeTypeExtension = 1  
	NodeTypeLeaf     = 2
)

// Trie represents a Patricia Merkle Trie
type Trie struct {
	db   database.Database
	root *Node
}

// Node represents a trie node
type Node struct {
	Type     int           `json:"type"`
	Key      []byte        `json:"key,omitempty"`
	Value    []byte        `json:"value,omitempty"`
	Children map[byte]*Node `json:"children,omitempty"`
	Hash     [32]byte      `json:"hash"`
	Dirty    bool          `json:"-"`
}

// NewTrie creates a new trie
func NewTrie(root [32]byte, db database.Database) (*Trie, error) {
	trie := &Trie{
		db: db,
	}
	
	// Load root node if exists
	if root != ([32]byte{}) {
		node, err := trie.loadNode(root)
		if err != nil {
			return nil, fmt.Errorf("failed to load root node: %v", err)
		}
		trie.root = node
	}
	
	return trie, nil
}

// Get retrieves a value from the trie
func (t *Trie) Get(key []byte) ([]byte, error) {
	if t.root == nil {
		return nil, nil
	}
	
	return t.get(t.root, hexToNibbles(key), 0)
}

// Update inserts or updates a value in the trie
func (t *Trie) Update(key, value []byte) error {
	if len(value) == 0 {
		return t.Delete(key)
	}
	
	nibbles := hexToNibbles(key)
	newRoot, err := t.update(t.root, nibbles, 0, value)
	if err != nil {
		return err
	}
	
	t.root = newRoot
	return nil
}

// Delete removes a key from the trie
func (t *Trie) Delete(key []byte) error {
	if t.root == nil {
		return nil
	}
	
	nibbles := hexToNibbles(key)
	newRoot, err := t.delete(t.root, nibbles, 0)
	if err != nil {
		return err
	}
	
	t.root = newRoot
	return nil
}

// Commit commits all pending changes to the database
func (t *Trie) Commit() ([32]byte, error) {
	if t.root == nil {
		return [32]byte{}, nil
	}
	
	return t.commitNode(t.root)
}

// Copy creates a deep copy of the trie
func (t *Trie) Copy() *Trie {
	newTrie := &Trie{
		db: t.db,
	}
	
	if t.root != nil {
		newTrie.root = t.copyNode(t.root)
	}
	
	return newTrie
}

// get retrieves value recursively
func (t *Trie) get(node *Node, key []byte, depth int) ([]byte, error) {
	if node == nil {
		return nil, nil
	}
	
	switch node.Type {
	case NodeTypeLeaf:
		if bytes.Equal(node.Key, key[depth:]) {
			return node.Value, nil
		}
		return nil, nil
		
	case NodeTypeExtension:
		if len(key) < depth+len(node.Key) {
			return nil, nil
		}
		if !bytes.Equal(node.Key, key[depth:depth+len(node.Key)]) {
			return nil, nil
		}
		
		// Navigate to child
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("extension node must have exactly one child")
		}
		
		var child *Node
		for _, c := range node.Children {
			child = c
			break
		}
		
		return t.get(child, key, depth+len(node.Key))
		
	case NodeTypeBranch:
		if depth >= len(key) {
			// End of key, return value if exists
			return node.Value, nil
		}
		
		// Navigate to appropriate child
		nextNibble := key[depth]
		child := node.Children[nextNibble]
		return t.get(child, key, depth+1)
		
	default:
		return nil, fmt.Errorf("unknown node type: %d", node.Type)
	}
}

// update inserts/updates value recursively
func (t *Trie) update(node *Node, key []byte, depth int, value []byte) (*Node, error) {
	if node == nil {
		// Create new leaf node
		return &Node{
			Type:  NodeTypeLeaf,
			Key:   key[depth:],
			Value: value,
			Dirty: true,
		}, nil
	}
	
	switch node.Type {
	case NodeTypeLeaf:
		existingKey := node.Key
		remainingKey := key[depth:]
		
		if bytes.Equal(existingKey, remainingKey) {
			// Update existing leaf
			newNode := t.copyNode(node)
			newNode.Value = value
			newNode.Dirty = true
			return newNode, nil
		}
		
		// Split leaf node
		commonPrefix := commonPrefixLength(existingKey, remainingKey)
		
		// Create branch node
		branch := &Node{
			Type:     NodeTypeBranch,
			Children: make(map[byte]*Node),
			Dirty:    true,
		}
		
		// Add existing leaf
		if commonPrefix < len(existingKey) {
			existingLeaf := &Node{
				Type:  NodeTypeLeaf,
				Key:   existingKey[commonPrefix+1:],
				Value: node.Value,
				Dirty: true,
			}
			branch.Children[existingKey[commonPrefix]] = existingLeaf
		} else {
			branch.Value = node.Value
		}
		
		// Add new value
		if commonPrefix < len(remainingKey) {
			newLeaf := &Node{
				Type:  NodeTypeLeaf,
				Key:   remainingKey[commonPrefix+1:],
				Value: value,
				Dirty: true,
			}
			branch.Children[remainingKey[commonPrefix]] = newLeaf
		} else {
			branch.Value = value
		}
		
		// Add extension if needed
		if commonPrefix > 0 {
			extension := &Node{
				Type:     NodeTypeExtension,
				Key:      remainingKey[:commonPrefix],
				Children: map[byte]*Node{0: branch},
				Dirty:    true,
			}
			return extension, nil
		}
		
		return branch, nil
		
	case NodeTypeExtension:
		extensionKey := node.Key
		remainingKey := key[depth:]
		
		commonPrefix := commonPrefixLength(extensionKey, remainingKey)
		
		if commonPrefix == len(extensionKey) {
			// Traverse through extension
			var child *Node
			for _, c := range node.Children {
				child = c
				break
			}
			
			newChild, err := t.update(child, key, depth+len(extensionKey), value)
			if err != nil {
				return nil, err
			}
			
			newNode := t.copyNode(node)
			newNode.Children = map[byte]*Node{0: newChild}
			newNode.Dirty = true
			return newNode, nil
		}
		
		// Split extension
		branch := &Node{
			Type:     NodeTypeBranch,
			Children: make(map[byte]*Node),
			Dirty:    true,
		}
		
		// Add shortened extension or direct child
		var child *Node
		for _, c := range node.Children {
			child = c
			break
		}
		
		if commonPrefix+1 < len(extensionKey) {
			// Create new extension for remaining part
			newExtension := &Node{
				Type:     NodeTypeExtension,
				Key:      extensionKey[commonPrefix+1:],
				Children: map[byte]*Node{0: child},
				Dirty:    true,
			}
			branch.Children[extensionKey[commonPrefix]] = newExtension
		} else {
			branch.Children[extensionKey[commonPrefix]] = child
		}
		
		// Add new value
		if commonPrefix+1 < len(remainingKey) {
			newLeaf := &Node{
				Type:  NodeTypeLeaf,
				Key:   remainingKey[commonPrefix+1:],
				Value: value,
				Dirty: true,
			}
			branch.Children[remainingKey[commonPrefix]] = newLeaf
		} else {
			branch.Value = value
		}
		
		// Add extension for common prefix if needed
		if commonPrefix > 0 {
			extension := &Node{
				Type:     NodeTypeExtension,
				Key:      remainingKey[:commonPrefix],
				Children: map[byte]*Node{0: branch},
				Dirty:    true,
			}
			return extension, nil
		}
		
		return branch, nil
		
	case NodeTypeBranch:
		newNode := t.copyNode(node)
		
		if depth >= len(key) {
			// Update branch value
			newNode.Value = value
			newNode.Dirty = true
			return newNode, nil
		}
		
		// Update child
		nextNibble := key[depth]
		child := newNode.Children[nextNibble]
		
		newChild, err := t.update(child, key, depth+1, value)
		if err != nil {
			return nil, err
		}
		
		newNode.Children[nextNibble] = newChild
		newNode.Dirty = true
		return newNode, nil
		
	default:
		return nil, fmt.Errorf("unknown node type: %d", node.Type)
	}
}

// delete removes key recursively
func (t *Trie) delete(node *Node, key []byte, depth int) (*Node, error) {
	if node == nil {
		return nil, nil
	}
	
	switch node.Type {
	case NodeTypeLeaf:
		if bytes.Equal(node.Key, key[depth:]) {
			return nil, nil // Delete leaf
		}
		return node, nil // Key not found
		
	case NodeTypeExtension:
		if len(key) < depth+len(node.Key) {
			return node, nil
		}
		if !bytes.Equal(node.Key, key[depth:depth+len(node.Key)]) {
			return node, nil
		}
		
		var child *Node
		for _, c := range node.Children {
			child = c
			break
		}
		
		newChild, err := t.delete(child, key, depth+len(node.Key))
		if err != nil {
			return nil, err
		}
		
		if newChild == nil {
			return nil, nil // Delete extension
		}
		
		newNode := t.copyNode(node)
		newNode.Children = map[byte]*Node{0: newChild}
		newNode.Dirty = true
		return newNode, nil
		
	case NodeTypeBranch:
		newNode := t.copyNode(node)
		
		if depth >= len(key) {
			// Delete branch value
			newNode.Value = nil
		} else {
			// Delete from child
			nextNibble := key[depth]
			child := newNode.Children[nextNibble]
			
			newChild, err := t.delete(child, key, depth+1)
			if err != nil {
				return nil, err
			}
			
			if newChild == nil {
				delete(newNode.Children, nextNibble)
			} else {
				newNode.Children[nextNibble] = newChild
			}
		}
		
		// Check if branch should be collapsed
		if len(newNode.Children) == 0 && newNode.Value == nil {
			return nil, nil
		}
		
		if len(newNode.Children) == 1 && newNode.Value == nil {
			// Convert to extension
			var childKey byte
			var child *Node
			for k, c := range newNode.Children {
				childKey = k
				child = c
				break
			}
			
			extension := &Node{
				Type:     NodeTypeExtension,
				Key:      []byte{childKey},
				Children: map[byte]*Node{0: child},
				Dirty:    true,
			}
			return extension, nil
		}
		
		newNode.Dirty = true
		return newNode, nil
		
	default:
		return nil, fmt.Errorf("unknown node type: %d", node.Type)
	}
}

// commitNode commits a node and its children to database
func (t *Trie) commitNode(node *Node) ([32]byte, error) {
	if node == nil {
		return [32]byte{}, nil
	}
	
	// Commit children first
	newChildren := make(map[byte]*Node)
	for key, child := range node.Children {
		childHash, err := t.commitNode(child)
		if err != nil {
			return [32]byte{}, err
		}
		
		// Store child hash instead of full node
		newChildren[key] = &Node{Hash: childHash}
	}
	
	// Create node for serialization
	serialNode := &Node{
		Type:     node.Type,
		Key:      node.Key,
		Value:    node.Value,
		Children: newChildren,
	}
	
	// Serialize and hash node
	data, err := json.Marshal(serialNode)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to marshal node: %v", err)
	}
	
	hash := crypto.Keccak256Hash(data)
	
	// Store in database
	key := append([]byte("trie_"), hash[:]...)
	if err := t.db.Put(key, data); err != nil {
		return [32]byte{}, fmt.Errorf("failed to store node: %v", err)
	}
	
	// Update node hash
	node.Hash = hash
	node.Dirty = false
	
	return hash, nil
}

// loadNode loads a node from database
func (t *Trie) loadNode(hash [32]byte) (*Node, error) {
	key := append([]byte("trie_"), hash[:]...)
	data, err := t.db.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to load node: %v", err)
	}
	
	if data == nil {
		return nil, fmt.Errorf("node not found: %x", hash)
	}
	
	var node Node
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node: %v", err)
	}
	
	node.Hash = hash
	return &node, nil
}

// copyNode creates a deep copy of a node
func (t *Trie) copyNode(node *Node) *Node {
	if node == nil {
		return nil
	}
	
	newNode := &Node{
		Type:  node.Type,
		Hash:  node.Hash,
		Dirty: node.Dirty,
	}
	
	if node.Key != nil {
		newNode.Key = make([]byte, len(node.Key))
		copy(newNode.Key, node.Key)
	}
	
	if node.Value != nil {
		newNode.Value = make([]byte, len(node.Value))
		copy(newNode.Value, node.Value)
	}
	
	if node.Children != nil {
		newNode.Children = make(map[byte]*Node)
		for k, child := range node.Children {
			newNode.Children[k] = child // Shallow copy for efficiency
		}
	}
	
	return newNode
}

// Utility functions
func hexToNibbles(hex []byte) []byte {
	nibbles := make([]byte, len(hex)*2)
	for i, b := range hex {
		nibbles[i*2] = b / 16
		nibbles[i*2+1] = b % 16
	}
	return nibbles
}

func commonPrefixLength(a, b []byte) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	
	return minLen
}
