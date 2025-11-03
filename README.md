## Go Ronin

Official Golang execution layer implementation of the Ronin protocol. It is a fork of Go Ethereum –
[https://github.com/ethereum/go-ethereum](https://github.com/ethereum/go-ethereum) – and is EVM compatible.

Ronin consensus currently uses a Delegated Proof of Stake mechanism, allowing anyone to become a validator.

Check out the [whitepaper](https://docs.roninchain.com/basics/white-paper) for more information.

[![Discord](https://img.shields.io/badge/discord-join%20chat-blue.svg)](https://discord.com/invite/pjgPrrZJyZ)

## Executables

The Go Ethereum project comes with several wrappers/executables found in the `cmd`
directory.

| Command | Description |
| :------ | :----------- |
| **`ronin`** | The main Ronin CLI client. It is the entry point into the Ronin network (main, test, or private net), capable of running as a full node (default), an archive node (retaining all historical state), or a light node (retrieving data live). It can be used by other processes as a gateway into the Ronin network via JSON-RPC endpoints exposed over HTTP, WebSocket, and/or IPC transports. Run `ronin --help` and check the [CLI page](https://geth.ethereum.org/docs/interface/command-line-options) for available options. |
| `clef` | Standalone signing tool, which can be used as a backend signer for `ronin`. |
| `devp2p` | Utilities to interact with nodes on the networking layer without running a full blockchain. |
| `abigen` | Source code generator that converts Ethereum contract definitions into easy-to-use, compile-time type-safe Go packages. It operates on plain [Ethereum contract ABIs](https://docs.soliditylang.org/en/develop/abi-spec.html), with expanded functionality if the contract bytecode is also available. It also accepts Solidity source files, making development much more streamlined. See our [Native DApps](https://geth.ethereum.org/docs/dapp/native-bindings) page for details. |
| `bootnode` | A stripped-down version of the Ethereum client implementation that only participates in the network node discovery protocol but does not run any higher-level application protocols. It can be used as a lightweight bootstrap node to help find peers in private networks. |
| `evm` | Developer utility version of the EVM (Ethereum Virtual Machine) capable of running bytecode snippets within a configurable environment and execution mode. Useful for isolated, fine-grained debugging of EVM opcodes (e.g. `evm --code 60ff60ff --debug run`). |
| `rlpdump` | Developer tool to convert binary RLP ([Recursive Length Prefix](https://eth.wiki/en/fundamentals/rlp)) dumps (data encoding used by the Ethereum protocol for both networking and consensus) into a more readable hierarchical representation (e.g. `rlpdump --hex CE0183FFFFFFC4C304050583616263`). |
| `puppeth` | A CLI wizard that helps create a new Ethereum network. |

## Running `ronin`

Going through all possible command-line flags is out of scope here (please consult our
[CLI Wiki page](https://geth.ethereum.org/docs/interface/command-line-options)),
but we've listed a few common parameter combinations to help you quickly
run your own `ronin` instance.

### Requirements

Running a full Ronin node:
- CPU: Equivalent of 8 AWS vCPUs  
- RAM: 16 GB  
- Storage: At least 1 TB high-speed SSD  
- Network: Reliable IPv4 or IPv6 connection with an open public port  

Running an archive Ronin node:
- CPU: Equivalent of 8 AWS vCPUs  
- RAM: 16 GB  
- Storage: At least 5 TB high-speed SSD  
- Network: Reliable IPv4 or IPv6 connection with an open public port  

### Building from source

Building `ronin` requires both Go (version 1.17 or later) and a C compiler. You can install
them using your favorite package manager. Once the dependencies are installed, run:

```shell
make ronin
```

or, to build the full suite of utilities:

```shell
make all
```

### Initializing genesis

Before running a full node, you must initialize the genesis block:

```shell
ronin init --datadir /opt/ronin genesis/mainnet.json
```

### Running a full node on the main Ronin network

```shell
ronin --http.api eth,net,web3,consortium --networkid 2020 --discovery.dns enrtree://AIGOFYDZH6BGVVALVJLRPHSOYJ434MPFVVQFXJDXHW5ZYORPTGKUI@nodes.roninchain.com --datadir /opt/ronin --port 30303 --http --http.corsdomain '*' --http.addr 0.0.0.0 --http.port 8545 --http.vhosts '*' --ws --ws.addr 0.0.0.0 --ws.port 8546 --ws.origins '*'
```

This command will:
* Start `ronin` in full sync mode (default, can be changed with the `--syncmode` flag),
  causing it to download more data in exchange for avoiding processing the entire history
  of the Ronin network, which is very CPU-intensive.

### Configuration

Instead of passing multiple flags to the `ronin` binary, you can also use a configuration file:

```shell
ronin --config /path/to/your_config.toml
```

To generate a reference configuration file, use the `dumpconfig` subcommand:

```shell
ronin --your-favourite-flags dumpconfig
```

### Programmatic access to `ronin` nodes

As a developer, you’ll likely want to interact with `ronin` and the Ronin network
programmatically rather than manually via the console.  
`ronin` has built-in support for JSON-RPC-based APIs identical to Ethereum’s:
([standard APIs](https://eth.wiki/json-rpc/API) and [`ronin`-specific APIs](https://geth.ethereum.org/docs/rpc/server)).
These can be exposed via HTTP, WebSocket, or IPC (UNIX sockets on UNIX-based systems,
and named pipes on Windows).

The IPC interface is enabled by default and exposes all APIs supported by `ronin`,  
while HTTP and WS interfaces must be manually enabled for security reasons.  

HTTP-based JSON-RPC API options:

* `--http` Enable the HTTP-RPC server  
* `--http.addr` HTTP-RPC listening interface (default: `localhost`)  
* `--http.port` HTTP-RPC listening port (default: `8545`)  
* `--http.api` APIs exposed via HTTP-RPC (default: `eth,net,web3`)  
* `--http.corsdomain` Comma-separated list of domains allowed for cross-origin requests  
* `--ws` Enable the WebSocket-RPC server  
* `--ws.addr` WebSocket-RPC listening interface (default: `localhost`)  
* `--ws.port` WebSocket-RPC listening port (default: `8546`)  
* `--ws.api` APIs exposed via WebSocket-RPC (default: `eth,net,web3`)  
* `--ws.origins` Origins allowed to connect via WebSocket  
* `--ipcdisable` Disable the IPC-RPC server  
* `--ipcapi` APIs exposed via IPC-RPC (default: `admin,debug,eth,miner,net,personal,shh,txpool,web3`)  
* `--ipcpath` Filename for the IPC socket/pipe within the datadir  

You’ll need to use your programming environment’s libraries or tools to connect
via HTTP, WS, or IPC to a `ronin` node configured with the above flags.  
All transports use the [JSON-RPC](https://www.jsonrpc.org/specification) protocol.

**Note:** Be aware of the security implications of exposing HTTP/WS transports.
Hackers actively scan the internet for open Ethereum-compatible RPCs.  
Locally running browsers can also access these, which may lead to unwanted access.

## How to contribute

### Contribution guidelines

- **Quality:** Code must follow style guidelines, include adequate test cases and descriptive commit messages, and ensure no compatibility issues or regressions.  
- **Size:** Prefer small, regular pull requests. Large PRs may be requested to be split into smaller, reviewable chunks.  
- **Maintainability:** If your feature requires ongoing maintenance (e.g., support for a specific database), you may be asked to maintain it.  
- **Commit messages:** Follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).  

### Submitting an issue

- Create a new issue.  
- Comment if you’d like to be assigned.  
- Browse issues labeled `help wanted` or `good first issue` for suitable starting points.  

### Submitting a PR

- Push your changes to your GitHub fork and submit a pull request (PR) to the `master` branch of the
  `axieinfinity/ronin` repository.  
- In your PR description, reference the related issue (see [linking a pull request to an issue](https://docs.github.com/en/issues/tracking-your-work-with-issues/linking-a-pull-request-to-an-issue-using-a-keyword)).  
  Example: `[FIXES #123] feat: update outdated content`  

### Review process

- The team reviews every PR.  
- Accepted PRs are approved and merged into the `master` branch.  

### Releases

- View the [release history](https://github.com/axieinfinity/ronin/releases) for highlights and details.

## License

The Go Ethereum library (i.e., all code outside the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in the repository as `COPYING.LESSER`.

The Go Ethereum binaries (i.e., all code inside the `cmd` directory) are licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html),
included as `COPYING`.
