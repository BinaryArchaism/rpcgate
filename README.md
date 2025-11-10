# rpcgate

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/BinaryArchaism/rpcgate/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/BinaryArchaism/rpcgate)](https://goreportcard.com/report/github.com/BinaryArchaism/rpcgate)

> **rpcgate** — self-hosted open-source proxy and load balancer for EVM RPC providers.

rpcgate lets you run your own lightweight gateway that connects to multiple Ethereum-compatible RPC providers.  
It increases reliability, provides unified access across chains, and exposes metrics for observability.

---

### ✨ Features
- **Multi-chain configuration** — define gateways for several networks in a single file.  
- **Multi-provider setup** — balance requests across public RPCs or private nodes for high availability.  
- **Metrics** — observe provider latency, error rates and usage.  
- **Client tracking** — per-application statistics.  

### 🚀 Quick start

1. Clone repo.
    ```
    git clone https://github.com/BinaryArchaism/rpcgate.git
    cd rpcgate
    ```
2. Create config file.
3. Build an image.
    ```
    docker build -t rpcgate .
    ```
4. Run it.
    ```
    docker run -p port:8080 -v your-config-path:/config.yaml [-d] rpcgate
    ```

### Load balancing options
- **p2cewma**
  Adaptive algorithm based on Exponentially Weighted Moving Average (EWMA) latency, in-flight load, and penalties for providers errors.
- **round-robin**
  Simple rotation of requests across providers.

> #### **p2cewma** is a default option.
> The p2cewma algorithm automatically adapts to provider latency and reliability, giving higher throughput under variable RPC conditions.

To configure a balancing strategy, specify it per-chain in your config:
```yaml 
rpcs:
  - name: mainnet
    balancer_type: p2cewma # [p2cewma, round-robin]
  - name: base
    # omit balancer_type to use default (p2cewma)
```

#### Client tracking options
rpcgate can identify requests by client using either Basic Auth or a query parameter,
so you can track metrics per application without changing any code.
- **Basic Auth** 
    ```yaml
    clients:
      auth_required: false # default
      type: basic          # default
      clients: 
        - login: admin
          password: test   # optional
    ```
    Connection string examples: 
    - https://admin:test@rpcgate-url/1
    - https://admin:@rpcgate-url/1
    - https://admin@rpcgate-url/1

    If you don’t need a password, omit it.
    
    > 📝 **Note:** Some SDKs (like Web3.py) require a colon `:` after username even if no password is set.

- **Query parameter**
    ```yaml
    clients:
      type: query 
    ```
    Connection string example: 
    - https://rpcgate-url/1?client=admin

### 🪪 License

MIT — see [LICENSE](https://github.com/BinaryArchaism/rpcgate/blob/master/LICENSE)