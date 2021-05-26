# PulseChain Node

The repo holds the PulseChain fork of [Go-Ethereum](https://github.com/ethereum/go-ethereum) and [Binance Smart Chain](https://github.com/binance-chain/bsc). Credit to the wealth of upstream development this project is built upon.

PulseChain is a stateful fork of Ethereum running Proof of Staked Authority consensus system with the stated goals of increased performance and significantly reduced fees for the users of the ecosystem. As a stateful fork, copies of all Ethereum contracts, tokens, and user accounts at time of fork will exist in the PulseChain network.

Being a fork of Go-Ethereum, many of the Go-Ethereum binaries and tools you're familiar with remain the same (geth, bootnode, puppeth, etc).

The Proof of Staked Authority (PoSA) consensus engine developed for BSC is based on the Clique consensus engine detailed in [EIP-225](https://eips.ethereum.org/EIPS/eip-225), with validators tracking and selection being dictated by system contracts. Validator rotation on BSC is administered through cross-chain messages originating from Binance Chain. PulseChain simplifies this system by removing the dual chain complexity and by implementing validator staking and rotation as native system contracts that can be directly interacted with by the PulseChain users. Slashing logic ensures liveness, security, stability, and chain finality.

The PulseChain network will launch with a stable set of maintained validators. Tech-savvy PulseChain users are encouraged to deploy new independent validators that can be voted into the network consensus by the PulseChain users, aiding in the decentralization of the network.

## Key features

### Stateful Ethereum Fork
PulseChain brings all of the Ethereum state with it! As of block number _______ (TBD), Exact copies of all smart contracts, ERC-20 tokens, ERC-721 NFTs, and user accounts will exist on PulseChain. Because of the extent of applications and use cases deployed on the Ethereum mainnet, it's not possible to anticipate exactly how any cloned assets will be valued by the community. Some contracts and applications will work 100% as they do on Ethereum, other contracts such as centralized stable coins are unlikely to have the authoritative support behind them.

Eventually the relative value of these assets will equalize though market action, but it is expected that there will be a discovery period with high volatility at launch of the network.

### Proof of Staked Authority 
Although Proof-of-Work (PoW) has been proven as a mechanism to implement a decentralized network, it is not practical for new or small networks and requires a large number of participants and computational waste to maintain the security. 

Proof-of-Authority(PoA) provides defense against 51% attack, with improved efficiency and tolerance to certain levels of Byzantine players (malicious or hacked). 
The PoA protocol however is most criticized for being not as decentralized as PoW, as the validators, i.e. the nodes that take turns to produce blocks, have all the authorities and are prone to corruption and security attacks.

Other blockchains, such as EOS and Cosmos both, introduce different types of Deputy Proof of Stake (DPoS) to allow the token holders to vote and elect the validator set. It increases the decentralization and favors community governance. 

PulseChain inherits and modifies the Binance Smart Chain consensus engine, Parlia, which combines DPoS and PoA. The PulseChain consensus engine has the following properties:

1. Blocks are produced by a limited set of validators.
2. Validators take turns to produce blocks in a PoA manner, similar to Ethereum's Clique consensus engine.
3. Validator set are elected in and out based on a staking contracts implemented on PulseChain.
4. Validator set rotation occurs on a regular interval with applicable validators chosen from the staking contract (selecting the validators with the bonded stake)
5. The consensus engine will interact directly with the slash, staking, and validator system-contracts to achieve liveness and stability, revenue distribution, and validator rotation.

## Native Token

The native ETH token will become PLS on the PulseChain network. The PLS supply will be inflated 10,000x upon forking, with the extra supply being distributed to the users that sacrificed during the PulseChain sacrifice phase.

PLS will be used just as ETH is used on the Ethereum network for transaction fees, as well as for delegating stake to network validators.

## Building the source

Many of the below are the same as or similar to go-ethereum.

For prerequisites and detailed build instructions please read the [Installation Instructions](https://geth.ethereum.org/docs/install-and-build/installing-geth) on the wiki.

Building `geth` requires both a Go (version 1.13 or later) and a C compiler. You can install
them using your favourite package manager. Once the dependencies are installed, run

```shell
make geth
```

or, to build the full suite of utilities:

```shell
make all
```

## Executables

The PulseChain project comes with several wrappers/executables found in the `cmd`
directory.

|    Command    | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| :-----------: | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
|  **`geth`**   | Main PulseChain client binary. It is the entry point into the Pulse network (main-, test- or private net), capable of running as a full node (default), archive node (retaining all historical state) or a light node (retrieving data live). It has the same and more RPC and other interface as go-ethereum and can be used by other processes as a gateway into the BSC network via JSON RPC endpoints exposed on top of HTTP, WebSocket and/or IPC transports. `geth --help` and the [CLI Wiki page](https://geth.ethereum.org/docs/interface/command-line-options) for command line options.          |
|   `abigen`    | Source code generator to convert Ethereum contract definitions into easy to use, compile-time type-safe Go packages. It operates on plain [Ethereum contract ABIs](https://github.com/ethereum/wiki/wiki/Ethereum-Contract-ABI) with expanded functionality if the contract bytecode is also available. However, it also accepts Solidity source files, making development much more streamlined. Please see our [Native DApps](https://github.com/ethereum/go-ethereum/wiki/Native-DApps:-Go-bindings-to-Ethereum-contracts) wiki page for details. |
|  `bootnode`   | Stripped down version of our Ethereum client implementation that only takes part in the network node discovery protocol, but does not run any of the higher level application protocols. It can be used as a lightweight bootstrap node to aid in finding peers in private networks.                                                                                                                                                                                                                                                                 |
|     `evm`     | Developer utility version of the EVM (Ethereum Virtual Machine) that is capable of running bytecode snippets within a configurable environment and execution mode. Its purpose is to allow isolated, fine-grained debugging of EVM opcodes (e.g. `evm --code 60ff60ff --debug run`).                                                                                                                                                                                                                                                                     |
| `gethrpctest` | Developer utility tool to support our [ethereum/rpc-test](https://github.com/ethereum/rpc-tests) test suite which validates baseline conformity to the [Ethereum JSON RPC](https://github.com/ethereum/wiki/wiki/JSON-RPC) specs. Please see the [test suite's readme](https://github.com/ethereum/rpc-tests/blob/master/README.md) for details.                                                                                                                                                                                                     |
|   `rlpdump`   | Developer utility tool to convert binary RLP ([Recursive Length Prefix](https://github.com/ethereum/wiki/wiki/RLP)) dumps (data encoding used by the Ethereum protocol both network as well as consensus wise) to user-friendlier hierarchical representation (e.g. `rlpdump --hex CE0183FFFFFFC4C304050583616263`).                                                                                                                                                                                                                                 |
|   `puppeth`   | a CLI wizard that aids in creating a new Ethereum network.

## Running `geth`

Going through all the possible command line flags is out of scope here (please consult our
[CLI Wiki page](https://github.com/ethereum/go-ethereum/wiki/Command-Line-Options)).

### Hardware Requirements

The hardware must meet certain requirements to run a full node.
- VPS running recent versions of Mac OS X or Linux.
- 500 GB of free disk space
- 8 cores of CPU and 16 gigabytes of memory (RAM) for mainnet.
- 4 cores of CPU and 8 gigabytes of memory (RAM) for testnet.
- A broadband Internet connection with upload/download speeds of at least 1 megabyte per second

### A Full node on the PulseChain Testnet

> **TODO** Provide instructions here once the PulseChain testnet is live.

*Note: Although there are some internal protective measures to prevent transactions from
crossing over between the main network and test network, you should make sure to always
use separate accounts for play-money and real-money. Unless you manually move
accounts, `geth` will by default correctly separate the two networks and will not make any
accounts available between them.*

### Programmatically interfacing `geth` nodes

As a developer, sooner rather than later you'll want to start interacting with `geth` and the
PulseChain network via your own programs and not manually through the console. To aid
this, `geth` has built-in support for a JSON-RPC based APIs ([standard APIs](https://github.com/ethereum/wiki/wiki/JSON-RPC)
and [`geth` specific APIs](https://github.com/ethereum/go-ethereum/wiki/Management-APIs)).
These can be exposed via HTTP, WebSockets and IPC (UNIX sockets on UNIX based
platforms, and named pipes on Windows).

The IPC interface is enabled by default and exposes all the APIs supported by `geth`,
whereas the HTTP and WS interfaces need to manually be enabled and only expose a
subset of APIs due to security reasons. These can be turned on/off and configured as
you'd expect.

HTTP based JSON-RPC API options:

  * `--rpc` Enable the HTTP-RPC server
  * `--rpcaddr` HTTP-RPC server listening interface (default: `localhost`)
  * `--rpcport` HTTP-RPC server listening port (default: `8545`)
  * `--rpcapi` API's offered over the HTTP-RPC interface (default: `eth,net,web3`)
  * `--rpccorsdomain` Comma separated list of domains from which to accept cross origin requests (browser enforced)
  * `--ws` Enable the WS-RPC server
  * `--wsaddr` WS-RPC server listening interface (default: `localhost`)
  * `--wsport` WS-RPC server listening port (default: `8546`)
  * `--wsapi` API's offered over the WS-RPC interface (default: `eth,net,web3`)
  * `--wsorigins` Origins from which to accept websockets requests
  * `--ipcdisable` Disable the IPC-RPC server
  * `--ipcapi` API's offered over the IPC-RPC interface (default: `admin,debug,eth,miner,net,personal,shh,txpool,web3`)
  * `--ipcpath` Filename for IPC socket/pipe within the datadir (explicit paths escape it)

You'll need to use your own programming environments' capabilities (libraries, tools, etc) to
connect via HTTP, WS or IPC to a `geth` node configured with the above flags and you'll
need to speak [JSON-RPC](https://www.jsonrpc.org/specification) on all transports. You
can reuse the same connection for multiple requests!

**Note: Please understand the security implications of opening up an HTTP/WS based
transport before doing so! Hackers on the internet are actively trying to subvert
PulseChain nodes with exposed APIs! Further, all browser tabs can access locally
running web servers, so malicious web pages could try to subvert locally available
APIs!**

## License

The PulseChain Node library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in our repository in the `COPYING.LESSER` file.

The PulseChain Node binaries (i.e. all code inside of the `cmd` directory) is licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `COPYING` file.
