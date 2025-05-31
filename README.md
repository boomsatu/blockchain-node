
# Blockchain Node Setup Guide

## Prerequisites

- Go 1.21 or higher
- Git

## Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd blockchain-node
```

2. Install dependencies:
```bash
go mod download
```

3. Build the application:
```bash
go build -o blockchain-node
```

## Configuration

Create a configuration file `config.yaml`:

```yaml
datadir: "./data"
port: 8080
rpcport: 8545
rpcaddr: "127.0.0.1"
mining: false
miner: ""
maxpeers: 50
chainid: 1337
blockgaslimit: 8000000
```

## Running the Node

### Start a Node
```bash
./blockchain-node startnode
```

### Start with Mining
```bash
./blockchain-node startnode --mining --miner 0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C
```

### Create a Wallet
```bash
./blockchain-node createwallet
```

### Check Balance
```bash
./blockchain-node getbalance 0x742d35Cc6635C0532925a3b8D5c6C1C8b1c5C6C
```

### Send Transaction
```bash
./blockchain-node send \
  --from 0xFromAddress \
  --to 0xToAddress \
  --amount 1000000000000000000 \
  --gaslimit 21000 \
  --gasprice 20000000000
```

## Environment Variables

- `BLOCKCHAIN_DATADIR`: Data directory (default: ./data)
- `BLOCKCHAIN_PORT`: P2P port (default: 8080)
- `BLOCKCHAIN_RPCPORT`: RPC port (default: 8545)
- `BLOCKCHAIN_RPCADDR`: RPC address (default: 127.0.0.1)

## Directory Structure

```
data/
├── chaindata/          # Blockchain data
├── keystore/           # Wallet files
└── nodes/             # Node information
```

## Connecting with Web3

You can connect to the node using standard Web3 libraries:

```javascript
const Web3 = require('web3');
const web3 = new Web3('http://localhost:8545');

// Get latest block
const block = await web3.eth.getBlock('latest');
console.log(block);
```

## Docker Support

Build Docker image:
```bash
docker build -t blockchain-node .
```

Run with Docker:
```bash
docker run -p 8080:8080 -p 8545:8545 blockchain-node
```

## Troubleshooting

### Common Issues

1. **Port already in use**: Change the port in config.yaml
2. **Permission denied**: Ensure data directory is writable
3. **Connection refused**: Check if the node is running and ports are open

### Logs

The node outputs logs to stdout. For persistent logging:
```bash
./blockchain-node startnode > node.log 2>&1 &
```

### Debug Mode

Run with verbose logging:
```bash
./blockchain-node startnode --verbosity 5
```
