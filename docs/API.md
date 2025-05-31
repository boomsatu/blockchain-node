
# Blockchain Node API Documentation

## Overview
This blockchain node provides a complete Ethereum-compatible JSON-RPC API and REST API for interacting with the blockchain.

## Base URLs
- JSON-RPC: `http://localhost:8545`
- REST API: `http://localhost:8545/api`

## REST API Endpoints

### Node Administration

#### POST /api/admin/start
Start the blockchain node with configuration.

**Request Body:**
```json
{
  "dataDir": "./data",
  "chainId": 1337,
  "port": 30303,
  "rpcPort": 8545,
  "blockGasLimit": 8000000,
  "mining": false,
  "miner": ""
}
```

**Response:**
```json
{
  "success": true,
  "message": "Node started successfully",
  "status": {
    "running": true,
    "startTime": "2024-01-01T00:00:00Z",
    "config": {...}
  }
}
```

#### POST /api/admin/stop
Stop the blockchain node.

**Response:**
```json
{
  "success": true,
  "message": "Node stopped successfully"
}
```

#### GET /api/admin/status
Get current node status.

**Response:**
```json
{
  "status": "ok",
  "config": {...},
  "running": true,
  "startTime": 1704067200,
  "uptime": 3600
}
```

#### POST /api/admin/config
Update node configuration.

**Request Body:**
```json
{
  "dataDir": "./data",
  "chainId": 1337,
  "blockGasLimit": 8000000
}
```

### Mining Control

#### POST /api/mining/start
Start mining process.

**Request Body:**
```json
{
  "minerAddress": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C",
  "threads": 1
}
```

**Response:**
```json
{
  "success": true,
  "message": "Mining started successfully",
  "stats": {
    "isActive": true,
    "hashRate": 0,
    "blocksFound": 0,
    "difficulty": "1000",
    "minerAddress": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C",
    "startTime": 1704067200
  }
}
```

#### POST /api/mining/stop
Stop mining process.

**Response:**
```json
{
  "success": true,
  "message": "Mining stopped successfully"
}
```

#### GET /api/mining/stats
Get current mining statistics.

**Response:**
```json
{
  "isActive": true,
  "hashRate": 123.45,
  "blocksFound": 5,
  "difficulty": "1000",
  "minerAddress": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C",
  "startTime": 1704067200
}
```

#### POST /api/mining/mine-block
Mine a single block manually.

**Request Body:**
```json
{
  "minerAddress": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C"
}
```

**Response:**
```json
{
  "blockNumber": 123,
  "hash": "0x1234567890abcdef...",
  "success": true
}
```

### Wallet Management

#### POST /api/wallet/create
Create a new wallet.

**Response:**
```json
{
  "address": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C",
  "privateKey": "0x1234567890abcdef...",
  "balance": "0x0"
}
```

#### POST /api/wallet/import
Import wallet from private key.

**Request Body:**
```json
{
  "privateKey": "0x1234567890abcdef..."
}
```

**Response:**
```json
{
  "address": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C",
  "privateKey": "0x1234567890abcdef...",
  "balance": "0x56bc75e2d630eb20"
}
```

#### POST /api/wallet/send
Send a transaction.

**Request Body:**
```json
{
  "from": "0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C",
  "to": "0x8ba1f109551bD432803012645Hac136c776dF7",
  "value": "0x56bc75e2d630eb20",
  "gasLimit": "0x5208",
  "gasPrice": "0x4a817c800",
  "privateKey": "0x1234567890abcdef...",
  "data": "0x"
}
```

**Response:**
```json
{
  "hash": "0xabcdef1234567890...",
  "success": true
}
```

### Network Information

#### GET /api/network/stats
Get network statistics.

**Response:**
```json
{
  "peerCount": 5,
  "blockHeight": 123,
  "difficulty": "1000",
  "hashRate": "0",
  "chainId": 1337,
  "syncStatus": {
    "isSyncing": false,
    "currentBlock": 123,
    "highestBlock": 123
  }
}
```

#### GET /api/network/peers
Get connected peers.

**Response:**
```json
[
  {
    "id": "peer1",
    "address": "192.168.1.100:30303",
    "version": "1.0.0"
  }
]
```

### Metrics

#### GET /api/metrics
Get node metrics.

**Response:**
```json
{
  "uptime": 1704067200,
  "memoryUsage": 52428800,
  "diskUsage": 0,
  "cpuUsage": 0,
  "blockCount": 124,
  "transactionCount": 456,
  "peersConnected": 5,
  "gasUsed": 0,
  "gasLimit": 8000000,
  "pendingTxs": 2
}
```

## JSON-RPC Methods

### Block Information

#### eth_blockNumber
Returns the number of most recent block.

**Parameters:** None

**Returns:** `QUANTITY` - integer of the current block number the client is on.

**Example:**
```bash
curl -X POST --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  -H "Content-Type: application/json" http://localhost:8545
```

#### eth_getBlockByNumber
Returns information about a block by block number.

**Parameters:**
1. `QUANTITY|TAG` - integer of a block number, or the string "latest"
2. `Boolean` - If true it returns the full transaction objects, if false only the hashes

**Returns:** `Object` - A block object

**Example:**
```bash
curl -X POST --data '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest", true],"id":1}' \
  -H "Content-Type: application/json" http://localhost:8545
```

#### eth_getBlockByHash
Returns information about a block by hash.

**Parameters:**
1. `DATA` - Hash of a block
2. `Boolean` - If true it returns the full transaction objects, if false only the hashes

### Account Information

#### eth_getBalance
Returns the balance of the account of given address.

**Parameters:**
1. `DATA` - 20 Bytes - address to check for balance
2. `QUANTITY|TAG` - integer block number, or the string "latest"

**Returns:** `QUANTITY` - integer of the current balance in wei

**Example:**
```bash
curl -X POST --data '{"jsonrpc":"2.0","method":"eth_getBalance","params":["0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C", "latest"],"id":1}' \
  -H "Content-Type: application/json" http://localhost:8545
```

#### eth_getTransactionCount
Returns the number of transactions sent from an address.

**Parameters:**
1. `DATA` - 20 Bytes - address
2. `QUANTITY|TAG` - integer block number, or the string "latest"

**Returns:** `QUANTITY` - integer of the number of transactions send from this address

### Transaction Information

#### eth_getTransactionByHash
Returns the information about a transaction requested by transaction hash.

**Parameters:**
1. `DATA` - 32 Bytes - hash of a transaction

**Returns:** `Object` - A transaction object

#### eth_getTransactionReceipt
Returns the receipt of a transaction by transaction hash.

**Parameters:**
1. `DATA` - 32 Bytes - hash of a transaction

**Returns:** `Object` - A transaction receipt object

#### eth_sendRawTransaction
Creates new message call transaction or a contract creation for signed transactions.

**Parameters:**
1. `DATA` - The signed transaction data

**Returns:** `DATA` - 32 Bytes - the transaction hash

### Contract Interaction

#### eth_call
Executes a new message call immediately without creating a transaction on the block chain.

**Parameters:**
1. `Object` - The transaction call object
2. `QUANTITY|TAG` - integer block number, or the string "latest"

**Returns:** `DATA` - the return value of executed contract

#### eth_estimateGas
Generates and returns an estimate of how much gas is necessary to allow the transaction to complete.

**Parameters:**
1. `Object` - The transaction call object

**Returns:** `QUANTITY` - the amount of gas used

#### eth_getCode
Returns code at a given address.

**Parameters:**
1. `DATA` - 20 Bytes - address
2. `QUANTITY|TAG` - integer block number, or the string "latest"

**Returns:** `DATA` - the code from the given address

### Network Information

#### eth_chainId
Returns the chain ID of the current network.

**Returns:** `QUANTITY` - integer of the current chain id

#### eth_gasPrice
Returns the current price per gas in wei.

**Returns:** `QUANTITY` - integer of the current gas price in wei

#### net_version
Returns the current network id.

**Returns:** `String` - The current network id

#### web3_clientVersion
Returns the current client version.

**Returns:** `String` - The current client version

## Error Codes

- `-32700`: Parse error
- `-32600`: Invalid Request
- `-32601`: Method not found
- `-32602`: Invalid params
- `-32603`: Internal error

## Health Check

The node provides a health check endpoint:

```bash
curl http://localhost:8545/health
```

Returns:
```json
{"status": "ok"}
```

## CORS Support

All endpoints support CORS with the following headers:
- `Access-Control-Allow-Origin: *`
- `Access-Control-Allow-Methods: GET, POST, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type`

## Rate Limiting

The API implements rate limiting to prevent abuse. Default limits:
- 100 requests per minute per IP
- Burst capacity of 20 requests

## Authentication

Currently, the API does not require authentication. In production environments, consider implementing proper authentication and authorization mechanisms.
