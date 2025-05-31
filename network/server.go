package network

import (
	"blockchain-node/core"
	"blockchain-node/interfaces"
	"blockchain-node/logger"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

type Server struct {
	port       int
	blockchain *core.Blockchain
	peers      map[string]*Peer
	listener   net.Listener
	running    bool
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc

	nodeKey    *ecdsa.PrivateKey
	myEnodeURL string
	bootNodes  []string
}

type Peer struct {
	conn            net.Conn
	address         string
	nodeID          string // Node ID Hex dari peer (setelah handshake)
	inbound         bool
	protocolVersion uint32
	networkID       uint64 // Menyimpan ChainID dari peer (sebelumnya mungkin salah tulis sebagai chainID)
	genesisHash     [32]byte
	latestBlockHash [32]byte
	totalDifficulty *big.Int
	encoder         *json.Encoder
	decoder         *json.Decoder
	lastPing        time.Time
	lastPong        time.Time
}

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type StatusMessage struct {
	ProtocolVersion uint32   `json:"protocolVersion"`
	NetworkID       uint64   `json:"networkID"` // Ini adalah ChainID
	TD              *big.Int `json:"td"`
	CurrentBlock    [32]byte `json:"currentBlock"`
	GenesisBlock    [32]byte `json:"genesisBlock"`
}

func NewServer(port int, blockchain *core.Blockchain, nodeKey *ecdsa.PrivateKey, myEnodeURL string, bootNodes []string) *Server {
	return &Server{
		port:       port,
		blockchain: blockchain,
		peers:      make(map[string]*Peer),
		nodeKey:    nodeKey,
		myEnodeURL: myEnodeURL,
		bootNodes:  bootNodes,
	}
}

func (s *Server) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to start P2P listener: %v", err)
	}
	s.listener = listener
	s.running = true
	logger.Infof("P2P server listening on port %d. My Enode (for others to connect): %s", s.port, s.myEnodeURL)

	go s.acceptConnections()

	if len(s.bootNodes) > 0 {
		logger.Infof("Attempting to connect to %d bootnode(s)...", len(s.bootNodes))
		go s.connectToBootnodes()
	}

	<-s.ctx.Done()
	return s.Stop()
}

func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	if s.listener != nil {
		s.listener.Close()
	}

	s.mu.Lock()
	for _, peer := range s.peers {
		if peer.conn != nil {
			peer.conn.Close()
		}
	}
	s.peers = make(map[string]*Peer)
	s.mu.Unlock()

	logger.Info("P2P server stopped")
	return nil
}

func (s *Server) connectToBootnodes() {
	for _, bootnodeEnode := range s.bootNodes {
		if !s.IsRunning() {
			return
		}

		trimmedEnode := strings.TrimPrefix(bootnodeEnode, "enode://")
		parts := strings.Split(trimmedEnode, "@")
		if len(parts) != 2 {
			logger.Warningf("Invalid bootnode enode URL format: %s", bootnodeEnode)
			continue
		}
		ipPort := parts[1]

		if strings.Contains(s.myEnodeURL, ipPort) {
			logger.Debugf("Skipping connection to self (bootnode): %s", bootnodeEnode)
			continue
		}

		s.mu.RLock()
		alreadyConnected := false
		for _, p := range s.peers {
			if p.address == ipPort {
				alreadyConnected = true
				break
			}
		}
		s.mu.RUnlock()
		if alreadyConnected {
			logger.Debugf("Already connected or attempting to connect to bootnode: %s", ipPort)
			continue
		}

		logger.Infof("Attempting to connect to bootnode: %s", ipPort)
		conn, err := net.DialTimeout("tcp", ipPort, 10*time.Second)
		if err != nil {
			logger.Warningf("Failed to connect to bootnode %s: %v", ipPort, err)
			continue
		}
		go s.handleConnection(conn, false)
	}
}

func (s *Server) acceptConnections() {
	for {
		if !s.IsRunning() {
			return
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if s.IsRunning() {
				logger.Warningf("Failed to accept incoming P2P connection: %v", err)
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
			} else {
				return
			}
			continue
		}
		go s.handleConnection(conn, true)
	}
}

func (s *Server) handleConnection(conn net.Conn, inbound bool) {
	remoteAddrStr := conn.RemoteAddr().String()
	logger.Infof("Handling new P2P connection with %s (inbound: %t)", remoteAddrStr, inbound)

	peer := &Peer{
		conn:    conn,
		address: remoteAddrStr,
		inbound: inbound,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
	}

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.peers, peer.address)
		s.mu.Unlock()
		logger.Infof("Peer %s disconnected. Total peers: %d", peer.address, s.GetPeerCount())
	}()

	if err := s.performHandshake(peer); err != nil {
		logger.Warningf("Handshake with %s failed: %v. Closing connection.", remoteAddrStr, err)
		return
	}

	for {
		if !s.IsRunning() {
			return
		}
		select {
		case <-s.ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(120 * time.Second))
			var msg Message
			if err := peer.decoder.Decode(&msg); err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					logger.Debugf("Read timeout from %s. Consider sending ping.", peer.address)
					continue
				}
				logger.Warningf("Error decoding message from peer %s: %v. Disconnecting.", peer.address, err)
				return
			}
			conn.SetReadDeadline(time.Time{})
			s.handleMessage(peer, &msg)
		}
	}
}

func (s *Server) performHandshake(peer *Peer) error {
	myStatus, err := s.prepareStatusMessage()
	if err != nil {
		return fmt.Errorf("failed to prepare own status message: %v", err)
	}
	if err := s.sendMessage(peer, &Message{Type: "status", Data: myStatus}); err != nil {
		return fmt.Errorf("failed to send status message to %s: %v", peer.address, err)
	}
	logger.Debugf("Sent status to %s", peer.address)

	var statusMsg Message
	peer.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := peer.decoder.Decode(&statusMsg); err != nil {
		return fmt.Errorf("failed to receive status message from peer %s: %v", peer.address, err)
	}
	peer.conn.SetReadDeadline(time.Time{})

	if statusMsg.Type != "status" {
		return fmt.Errorf("expected status message from %s, got %s", peer.address, statusMsg.Type)
	}
	logger.Debugf("Received status from %s: %+v", peer.address, statusMsg.Data)

	var peerStatusData StatusMessage
	jsonData, _ := json.Marshal(statusMsg.Data)
	if err := json.Unmarshal(jsonData, &peerStatusData); err != nil {
		return fmt.Errorf("failed to unmarshal peer status data from %s: %v", peer.address, err)
	}

	myChainID := s.blockchain.GetConfig().GetChainID()
	if peerStatusData.NetworkID != myChainID { // Menggunakan NetworkID dari peerStatusData
		s.sendMessage(peer, &Message{Type: "disconnect", Data: "chain ID mismatch"})
		return fmt.Errorf("chain ID mismatch with %s: mine %d, peer %d", peer.address, myChainID, peerStatusData.NetworkID)
	}

	myGenesisBlock := s.blockchain.GetBlockByNumber(0)
	if myGenesisBlock == nil {
		return errors.New("cannot perform handshake: local genesis block not found")
	}
	myGenesisHash := myGenesisBlock.Header.GetHash()
	if peerStatusData.GenesisBlock != myGenesisHash {
		s.sendMessage(peer, &Message{Type: "disconnect", Data: "genesis block mismatch"})
		return fmt.Errorf("genesis block hash mismatch with %s: mine %x, peer %x", peer.address, myGenesisHash, peerStatusData.GenesisBlock)
	}

	peer.networkID = peerStatusData.NetworkID // Menyimpan NetworkID (ChainID) peer
	peer.genesisHash = peerStatusData.GenesisBlock
	peer.latestBlockHash = peerStatusData.CurrentBlock
	peer.totalDifficulty = new(big.Int).Set(peerStatusData.TD)

	s.mu.Lock()
	if _, exists := s.peers[peer.address]; exists {
		s.mu.Unlock()
		logger.Warningf("Peer %s (or its address) already in map, closing redundant connection.", peer.address)
		return errors.New("peer already connected")
	}
	s.peers[peer.address] = peer
	s.mu.Unlock()
	logger.Infof("Handshake successful with %s. Peer TD: %s, My TD: %s. Total peers: %d",
		peer.address, peerStatusData.TD.String(), myStatus.TD.String(), s.GetPeerCount())

	if peerStatusData.TD.Cmp(myStatus.TD) > 0 {
		logger.Infof("Peer %s has higher TD. Initiating sync...", peer.address)
		go s.synchronizeWithPeer(peer)
	} else if myStatus.TD.Cmp(peerStatusData.TD) > 0 {
		logger.Infof("My TD is higher than peer %s. Peer might sync from us.", peer.address)
	} else {
		logger.Infof("TDs are equal with peer %s. No immediate sync needed.", peer.address)
	}
	return nil
}

func (s *Server) prepareStatusMessage() (*StatusMessage, error) {
	currentBlock := s.blockchain.GetCurrentBlock()
	genesisBlock := s.blockchain.GetBlockByNumber(0)

	if currentBlock == nil || genesisBlock == nil {
		return nil, errors.New("blockchain not fully initialized for status message (current or genesis is nil)")
	}

	// Perbaikan: Memanggil GetTotalDifficulty dari s.blockchain
	td := s.blockchain.GetTotalDifficulty() // Asumsi metode ini sudah ada di core.Blockchain
	if td == nil {
		logger.Warning("Failed to get total difficulty, using current block's difficulty as fallback for status.")
		td = new(big.Int).Set(currentBlock.Header.GetDifficulty())
	}

	currentBlockHash := currentBlock.Header.GetHash() // Simpan ke variabel
	genesisBlockHash := genesisBlock.Header.GetHash() // Simpan ke variabel

	return &StatusMessage{
		ProtocolVersion: 1,
		NetworkID:       s.blockchain.GetConfig().GetChainID(),
		TD:              td,
		CurrentBlock:    currentBlockHash, // Gunakan variabel
		GenesisBlock:    genesisBlockHash, // Gunakan variabel
	}, nil
}

func (s *Server) synchronizeWithPeer(peer *Peer) {
	logger.Infof("Starting synchronization with peer %s", peer.address)
	localCurrentBlock := s.blockchain.GetCurrentBlock()
	if localCurrentBlock == nil {
		logger.Warningf("Sync with %s: Local current block is nil.", peer.address)
		return
	}

	// Perbaikan: Simpan hasil GetHash ke variabel sebelum slicing
	localLatestHashBytes := localCurrentBlock.Header.GetHash()
	getHeadersMsg := &Message{
		Type: "getblockheaders",
		Data: map[string]interface{}{
			"originHash": hex.EncodeToString(localLatestHashBytes[:]), // Gunakan slice dari variabel
			"amount":     10,
		},
	}
	logger.Debugf("Sync with %s: Sending getblockheaders, origin %x", peer.address, localLatestHashBytes)
	s.sendMessage(peer, getHeadersMsg)
}

func (s *Server) handleMessage(peer *Peer, msg *Message) {
	logger.Debugf("Received message of type '%s' from peer %s", msg.Type, peer.address)
	switch msg.Type {
	case "status":
		logger.Infof("Received unexpected status message from %s outside handshake.", peer.address)
	case "getblockheaders":
		s.handleGetBlockHeaders(peer, msg)
	case "blockheaders":
		s.handleBlockHeaders(peer, msg)
	case "getblockbodies":
		s.handleGetBlockBodies(peer, msg)
	case "blockbodies":
		s.handleBlockBodies(peer, msg)
	case "transaction":
		s.handleTransaction(peer, msg)
	default:
		logger.Warningf("Unknown message type '%s' from peer %s", msg.Type, peer.address)
	}
}

func (s *Server) sendMessage(peer *Peer, msg *Message) error {
	if peer == nil || peer.conn == nil || peer.encoder == nil {
		return errors.New("cannot send message: peer or peer connection/encoder is nil")
	}
	peer.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := peer.encoder.Encode(msg)
	peer.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		logger.Warningf("Failed to send message type %s to peer %s: %v. Disconnecting.", msg.Type, peer.address, err)
		return err
	}
	return nil
}

func (s *Server) handleGetBlockHeaders(peer *Peer, msg *Message) {
	logger.Debugf("Peer %s requested block headers. Data: %+v", peer.address, msg.Data)
	var headersToSend []interfaces.BlockHeaderItf // Gunakan tipe interface
	startNum := uint64(1)                         // Contoh: mulai dari blok setelah genesis

	// Parse permintaan (asumsi Data adalah map[string]interface{})
	reqData, ok := msg.Data.(map[string]interface{})
	if !ok {
		logger.Warningf("Invalid data format for getblockheaders from %s", peer.address)
		return
	}
	originHashStr, _ := reqData["originHash"].(string)
	amountFloat, _ := reqData["amount"].(float64) // JSON unmarshal angka ke float64
	amount := int(amountFloat)
	if amount <= 0 || amount > 100 { // Batasi jumlah header
		amount = 10
	}

	originHashBytes, err := hex.DecodeString(originHashStr)
	if err != nil {
		logger.Warningf("Invalid originHash format from %s: %v", peer.address, err)
		return
	}
	var originHash [32]byte
	copy(originHash[:], originHashBytes)

	originBlock := s.blockchain.GetBlockByHash(originHash)
	if originBlock != nil {
		startNum = originBlock.Header.GetNumber() + 1
	} else if originHash != ([32]byte{}) { // Jika originHash bukan hash nol (genesis parent)
		logger.Warningf("Origin block %x not found for getblockheaders from %s", originHash, peer.address)
		// Mungkin kirim error atau tidak ada header
		s.sendMessage(peer, &Message{Type: "blockheaders", Data: []interfaces.BlockHeaderItf{}})
		return
	}

	for i := 0; i < amount; i++ {
		block := s.blockchain.GetBlockByNumber(startNum + uint64(i))
		if block == nil {
			break
		}
		headersToSend = append(headersToSend, block.Header)
	}

	if len(headersToSend) > 0 {
		logger.Debugf("Sending %d block headers to %s", len(headersToSend), peer.address)
		s.sendMessage(peer, &Message{Type: "blockheaders", Data: headersToSend})
	} else {
		logger.Debugf("No block headers to send to %s for the given request", peer.address)
		// Anda bisa mengirim array kosong atau tidak mengirim apa-apa
		s.sendMessage(peer, &Message{Type: "blockheaders", Data: []interfaces.BlockHeaderItf{}})
	}
}

func (s *Server) handleBlockHeaders(peer *Peer, msg *Message) {
	logger.Debugf("Received block headers from %s. Data: %+v", peer.address, msg.Data)
	// TODO: Parse msg.Data (array of block headers)
	// Validasi header, jika valid, minta badan bloknya dengan "getblockbodies"
}

func (s *Server) handleGetBlockBodies(peer *Peer, msg *Message) {
	logger.Debugf("Peer %s requested block bodies. Data: %+v", peer.address, msg.Data)
	// TODO: Parse msg.Data (array of block hashes)
	// Ambil badan blok dari blockchain dan kirim dengan pesan "blockbodies"
}

func (s *Server) handleBlockBodies(peer *Peer, msg *Message) {
	logger.Debugf("Received block bodies from %s. Data: %+v", peer.address, msg.Data)
	// TODO: Parse msg.Data (array of block bodies/transactions)
	// Gabungkan dengan header yang sudah ada, validasi blok penuh, tambahkan ke chain.
}

func (s *Server) handleTransaction(peer *Peer, msg *Message) {
	logger.Debugf("Received transaction message from %s", peer.address)
	var tx core.Transaction
	txDataBytes, err := json.Marshal(msg.Data)
	if err != nil {
		logger.Warningf("Failed to marshal transaction data from peer %s: %v", peer.address, err)
		return
	}
	if err := json.Unmarshal(txDataBytes, &tx); err != nil {
		logger.Warningf("Failed to unmarshal transaction from peer %s: %v", peer.address, err)
		return
	}
	if err := s.blockchain.AddTransaction(&tx); err != nil {
		txHash := tx.GetHash()
		logger.Warningf("Failed to add transaction %x from peer %s to mempool: %v", txHash, peer.address, err)
	} else {
		txHash := tx.GetHash()
		logger.Infof("Added transaction %x from peer %s to mempool.", txHash, peer.address)
		s.BroadcastTransaction(&tx, peer.address)
	}
}

func (s *Server) GetPeerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

func (s *Server) GetConnectionCount() int {
	return s.GetPeerCount()
}

func (s *Server) BroadcastBlock(block *core.Block, originPeerAddr string) {
	s.mu.RLock()
	peersToBroadcast := make([]*Peer, 0, len(s.peers))
	for addr, peer := range s.peers {
		if addr != originPeerAddr {
			peersToBroadcast = append(peersToBroadcast, peer)
		}
	}
	s.mu.RUnlock()

	msg := &Message{Type: "block", Data: block}
	for _, peer := range peersToBroadcast {
		if err := s.sendMessage(peer, msg); err != nil {
			logger.Warningf("Error broadcasting block to peer %s: %v", peer.address, err)
		}
	}
}

func (s *Server) BroadcastTransaction(tx *core.Transaction, originPeerAddr string) {
	s.mu.RLock()
	peersToBroadcast := make([]*Peer, 0, len(s.peers))
	for addr, peer := range s.peers {
		if addr != originPeerAddr {
			peersToBroadcast = append(peersToBroadcast, peer)
		}
	}
	s.mu.RUnlock()

	msg := &Message{Type: "transaction", Data: tx}
	for _, peer := range peersToBroadcast {
		if err := s.sendMessage(peer, msg); err != nil {
			logger.Warningf("Error broadcasting transaction to peer %s: %v", peer.address, err)
		}
	}
}

func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
