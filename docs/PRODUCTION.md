
# Production Deployment Guide

## Overview

This document provides comprehensive guidance for deploying the blockchain node in a production environment with focus on security, performance, and reliability.

## Security Features

### Rate Limiting
- Default: 100 requests per minute per IP
- Configurable via `rate_limit` and `rate_limit_window` settings
- Automatic IP blacklisting for repeated violations

### Input Validation
- Comprehensive transaction validation
- Block validation with size and gas limits
- Address format validation
- Signature verification

### Logging & Monitoring
- Structured logging with different levels (DEBUG, INFO, WARNING, ERROR, FATAL)
- Security event logging
- Transaction and block event logging
- Network event logging

## Performance Optimizations

### Caching
- In-memory cache for frequently accessed data
- Configurable cache size and TTL
- Automatic cleanup of expired entries

### Database
- LevelDB for high-performance key-value storage
- Configurable cache and handle limits
- Proper connection management

### Memory Management
- Regular memory usage monitoring
- Garbage collection optimization
- Resource cleanup on shutdown

## Configuration

### Environment Variables
All configuration options can be set via environment variables with `BLOCKCHAIN_` prefix:

```bash
export BLOCKCHAIN_DATADIR="/opt/blockchain/data"
export BLOCKCHAIN_PORT=8080
export BLOCKCHAIN_RPCPORT=8545
export BLOCKCHAIN_MINING=true
export BLOCKCHAIN_ENABLE_RATE_LIMIT=true
```

### Production Configuration Example

```yaml
# config.yaml
datadir: "/opt/blockchain/data"
port: 8080
rpcport: 8545
rpcaddr: "0.0.0.0"
mining: false
miner: ""
maxpeers: 100
chainid: 1
blockgaslimit: 10000000
cache: 512
handles: 512
verbosity: 2
enable_rate_limit: true
rate_limit: 1000
rate_limit_window: "1m"
enable_cache: true
cache_size: 10000
connection_timeout: "30s"
health_check_interval: "30s"
enable_metrics: true
```

## Deployment

### System Requirements
- **CPU**: 4+ cores recommended
- **Memory**: 8GB+ RAM
- **Storage**: 100GB+ SSD storage
- **Network**: Reliable internet connection

### Installation Steps

1. **Create dedicated user**:
```bash
sudo useradd -r -s /bin/false blockchain
sudo mkdir -p /opt/blockchain
sudo chown blockchain:blockchain /opt/blockchain
```

2. **Copy binary and configuration**:
```bash
sudo cp blockchain-node /opt/blockchain/
sudo cp config.yaml /opt/blockchain/
sudo chown blockchain:blockchain /opt/blockchain/*
```

3. **Create systemd service**:
```bash
sudo tee /etc/systemd/system/blockchain-node.service > /dev/null <<EOF
[Unit]
Description=Blockchain Node
After=network.target

[Service]
Type=simple
User=blockchain
WorkingDirectory=/opt/blockchain
ExecStart=/opt/blockchain/blockchain-node startnode --config /opt/blockchain/config.yaml
Restart=always
RestartSec=10
KillMode=process

[Install]
WantedBy=multi-user.target
EOF
```

4. **Enable and start service**:
```bash
sudo systemctl daemon-reload
sudo systemctl enable blockchain-node
sudo systemctl start blockchain-node
```

## Monitoring

### Health Checks
- **Health endpoint**: `http://localhost:9545/health`
- **Readiness endpoint**: `http://localhost:9545/ready`
- **Metrics endpoint**: `http://localhost:9545/metrics`

### Log Files
- **Application logs**: `logs/blockchain-YYYY-MM-DD.log`
- **System logs**: `journalctl -u blockchain-node -f`

### Key Metrics to Monitor
- Transaction throughput (TPS)
- Block production rate
- Memory usage
- Disk usage
- Network connectivity
- Error rates

## Security Hardening

### Network Security
- Use firewall to restrict access to necessary ports only
- Consider running behind a reverse proxy
- Enable TLS for RPC endpoints in production

### File System Security
```bash
# Secure data directory
sudo chmod 750 /opt/blockchain/data
sudo chown -R blockchain:blockchain /opt/blockchain/data

# Secure configuration
sudo chmod 640 /opt/blockchain/config.yaml
```

### Process Security
- Run as non-root user
- Use systemd for process management
- Enable automatic restarts

## Backup and Recovery

### Data Backup
```bash
# Stop the service
sudo systemctl stop blockchain-node

# Backup blockchain data
sudo tar -czf blockchain-backup-$(date +%Y%m%d).tar.gz /opt/blockchain/data/

# Restart the service
sudo systemctl start blockchain-node
```

### Recovery Process
1. Stop the blockchain service
2. Restore data from backup
3. Verify data integrity
4. Restart the service

## Troubleshooting

### Common Issues

1. **High memory usage**:
   - Reduce cache size in configuration
   - Monitor for memory leaks
   - Restart service if necessary

2. **Slow transaction processing**:
   - Check system resources
   - Verify network connectivity
   - Monitor peer connections

3. **Database corruption**:
   - Stop service immediately
   - Restore from latest backup
   - Check disk health

### Log Analysis
```bash
# Check for errors
journalctl -u blockchain-node --since "1 hour ago" | grep ERROR

# Monitor real-time logs
journalctl -u blockchain-node -f

# Check security events
grep "security_event" /opt/blockchain/logs/blockchain-*.log
```

## Performance Tuning

### Database Optimization
- Increase cache size for better read performance
- Use SSD storage for better I/O performance
- Monitor disk usage and implement log rotation

### Network Optimization
- Increase max peers for better connectivity
- Optimize connection timeouts
- Use dedicated network interface if possible

### Memory Optimization
- Tune garbage collection settings
- Monitor memory usage patterns
- Implement memory limits in systemd service

## Maintenance

### Regular Tasks
- Monitor disk usage
- Rotate log files
- Update security patches
- Backup blockchain data
- Monitor performance metrics

### Updates
1. Stop the service
2. Backup current data
3. Update binary
4. Update configuration if needed
5. Start service and verify operation

## Support

For production support issues:
1. Check logs for error messages
2. Verify system resources
3. Check network connectivity
4. Review configuration settings
5. Contact support with relevant logs and metrics
