
# Wallet Extension Guide

## Overview
The Blockchain Node includes a complete Chrome extension that provides Web3 wallet functionality, allowing users to interact with DApps and manage their blockchain accounts directly from their browser.

## Installation

### For Developers
1. Build the blockchain node project
2. Open Chrome and navigate to `chrome://extensions/`
3. Enable "Developer mode" in the top right
4. Click "Load unpacked"
5. Select the `wallet-extension` folder from the project

### For Users
1. Download the extension package (.crx file)
2. Open Chrome extensions page
3. Drag and drop the .crx file to install

## Features

### Core Functionality
- **Account Management**: Create new wallets or import existing ones
- **Transaction Sending**: Send ETH to any address with custom gas settings
- **Balance Viewing**: Real-time balance updates in ETH and USD
- **Message Signing**: Sign messages for DApp authentication
- **QR Code Support**: Generate QR codes for receiving funds

### Web3 Provider
The extension automatically injects a Web3 provider (`window.ethereum`) that supports:

- Standard Ethereum JSON-RPC methods
- Account management (`eth_accounts`, `eth_requestAccounts`)
- Transaction sending (`eth_sendTransaction`)
- Message signing (`personal_sign`)
- Network information (`eth_chainId`, `net_version`)
- Event notifications (account changes, network changes)

### DApp Compatibility
Compatible with popular DApps that use:
- MetaMask-style provider APIs
- Web3.js library
- Ethers.js library
- Standard Ethereum wallet interfaces

## User Guide

### Setting Up Your Wallet

#### Creating a New Wallet
1. Click the extension icon in Chrome toolbar
2. Click "Create New Wallet"
3. **Important**: Save your private key securely - you'll need it to restore your wallet
4. Your new address will be displayed

#### Importing Existing Wallet
1. Click "Import Wallet"
2. Enter your 64-character private key (without 0x prefix)
3. Click "Import"
4. Your wallet will be loaded with your existing address

### Sending Transactions
1. Ensure your wallet has sufficient balance
2. Click "Send" in the wallet interface
3. Enter recipient address (must start with 0x)
4. Enter amount in ETH (e.g., 0.1 for 0.1 ETH)
5. Adjust gas price if needed (default: 20 Gwei)
6. Click "Send" to confirm

### Receiving Funds
1. Click "Receive" to view your wallet address
2. Share this address with the sender
3. Copy the address using the "Copy Address" button
4. Funds sent to this address will appear in your balance

### Interacting with DApps
1. Navigate to any Ethereum DApp website
2. The extension automatically provides Web3 functionality
3. When the DApp requests account access, approve the connection
4. You can now interact with the DApp using your wallet

## Technical Details

### Supported RPC Methods
```javascript
// Account methods
eth_accounts
eth_requestAccounts
eth_getBalance
eth_getTransactionCount

// Network methods
eth_chainId
net_version
web3_clientVersion

// Transaction methods
eth_sendTransaction
eth_sendRawTransaction
eth_estimateGas
eth_gasPrice

// Contract methods
eth_call
eth_getCode
eth_getStorageAt

// Block and transaction info
eth_blockNumber
eth_getBlockByNumber
eth_getBlockByHash
eth_getTransactionByHash
eth_getTransactionReceipt

// Signing methods
personal_sign

// Utility methods
eth_getLogs
```

### Event System
The extension emits standard Ethereum events:

```javascript
// Account changes
window.ethereum.on('accountsChanged', (accounts) => {
  console.log('New accounts:', accounts);
});

// Network changes
window.ethereum.on('chainChanged', (chainId) => {
  console.log('New chain:', chainId);
});

// Connection status
window.ethereum.on('connect', (connectInfo) => {
  console.log('Connected:', connectInfo);
});

window.ethereum.on('disconnect', (error) => {
  console.log('Disconnected:', error);
});
```

## Configuration

### Network Settings
By default, the extension connects to `http://localhost:8545`. To connect to a different node:

1. Open the extension's background.js file
2. Modify the `rpcUrl` variable
3. Reload the extension

### Security Settings
- Private keys are stored locally in Chrome's storage
- Keys are never transmitted over the network
- All transactions require explicit user approval

## Troubleshooting

### Common Issues

#### Extension Not Appearing
- Verify the extension is properly loaded in Chrome
- Check that developer mode is enabled
- Refresh the browser tab

#### Cannot Connect to Node
- Ensure the blockchain node is running
- Verify the node is listening on the correct port (8545)
- Check firewall settings

#### Transactions Failing
- Verify sufficient balance for transaction + gas fees
- Check that the recipient address is valid
- Ensure gas price is adequate for network conditions

#### DApp Integration Issues
- Refresh the DApp page after installing the extension
- Check browser console for error messages
- Verify the DApp supports the network (Chain ID 1337)

### Debug Information
Enable debugging by opening Chrome DevTools:
1. Right-click on extension popup → "Inspect"
2. Check Console tab for error messages
3. Network tab shows RPC calls to the blockchain node

## Security Considerations

### Private Key Security
- Never share your private key with anyone
- Store your private key backup in a secure location
- Consider using a hardware wallet for large amounts

### Transaction Security
- Always verify recipient addresses before sending
- Double-check transaction amounts
- Understand gas fees before confirming transactions

### DApp Security
- Only interact with trusted DApps
- Review transaction details before approving
- Be cautious of applications requesting unusual permissions

## Development

### Architecture
The extension consists of several components:

- **Popup**: Main user interface (popup.html/popup.js)
- **Background**: Service worker for persistent operations (background.js)
- **Content Script**: Bridge between webpage and extension (content.js)
- **Injected Script**: Web3 provider implementation (inject.js)

### Extending Functionality
To add new features:

1. Modify the appropriate component files
2. Update manifest.json if adding new permissions
3. Test thoroughly with various DApps
4. Reload extension for changes to take effect

### Building for Production
1. Minify JavaScript files for smaller package size
2. Test extensively on various websites
3. Update version number in manifest.json
4. Package as .crx file or submit to Chrome Web Store

## API Reference

### Extension-specific Methods
```javascript
// Get wallet status
chrome.runtime.sendMessage({
  type: 'GET_WALLET'
}, (response) => {
  if (response.wallet) {
    console.log('Wallet address:', response.wallet.address);
  }
});

// Make direct RPC calls
chrome.runtime.sendMessage({
  type: 'RPC_CALL',
  method: 'eth_getBalance',
  params: ['0x...', 'latest']
}, (response) => {
  console.log('Balance:', response.result);
});
```

### Web3 Provider Usage
```javascript
// Check if extension is available
if (typeof window.ethereum !== 'undefined') {
  console.log('Blockchain Wallet is installed!');
}

// Request account access
const accounts = await window.ethereum.request({
  method: 'eth_requestAccounts'
});

// Send transaction
const txHash = await window.ethereum.request({
  method: 'eth_sendTransaction',
  params: [{
    from: accounts[0],
    to: '0x...',
    value: '0x38D7EA4C68000', // 0.001 ETH
    gas: '0x5208'
  }]
});
```

## Support

For additional help:
1. Check the troubleshooting section above
2. Review browser console for error messages
3. Ensure blockchain node is properly configured
4. Refer to the main project documentation

## Future Updates

Planned enhancements include:
- Multi-network support
- Enhanced security features
- Improved user interface
- Hardware wallet integration
- Advanced transaction management
