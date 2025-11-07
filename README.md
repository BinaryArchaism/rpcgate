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

WIP

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
    Connection string example: 
    - https://admin:test@rpcgate-url/1
    - https://admin:@rpcgate-url/1
    - https://admin@rpcgate-url/1

    > If you don’t need a password, omit it.
    
    > Some libraries (e.g. Web3.py) require a colon (:) after the username even if no password is used.

- **Query parameter**
    ```yaml
    clients:
      type: query 
    ```
    Connection string example: 
    - https://rpcgate-url/1?client=admin

### 🪪 License

MIT — see [LICENSE](https://github.com/BinaryArchaism/rpcgate/blob/master/LICENSE)