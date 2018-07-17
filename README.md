DEXON Consensus Core
====================

## Getting Started
### Prerequisites

- [Go 1.10](https://golang.org/dl/) or a newer version
- [dep](https://github.com/golang/dep#installation) as dependency management

### Installation

1. Clone the repo
    ```
    git clone https://github.com/dexon-foundation/dexon-consensus-core.git
    cd dexon-consensus-core
    ```

2. Install go dependency management tool
   ```
   ./bin/install_tools.sh
   ```

3. Install all dependencies
   ```
   dep ensure
   ```

4. Setup GOAPTH, the GOPATH could be anywhere in the system. Here we use `$HOME/go`:
   ```
   export GOPATH=$HOME/go
   export PATH=$GOPATH/bin:$PATH
   ```
   You should write these settings to your `.bashrc` file.

### Run unit tests

```
make test
```

## Simulation

1. Setup the configuration under `./test.toml`
2. Compile and install the cmd `dexon-simulation`

```
make
```

4. Run simulation:

```
dexcon-simulation -config test.toml -init
```
