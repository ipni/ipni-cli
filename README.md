# ipni-cli
[![](https://img.shields.io/badge/made%20by-Protocol%20Labs-blue.svg?style=flat-square)](https://protocol.ai)
[![Go Reference](https://pkg.go.dev/badge/github.com/ipni/ipni-cli.svg)](https://pkg.go.dev/github.com/ipni/ipni-cli)
[![Go Test](https://github.com/ipni/ipni-cli/actions/workflows/go-test.yml/badge.svg)](https://github.com/ipni/ipni-cli/actions/workflows/go-test.yml)

:computer: CLI tools for all things IPNI

This project provides a command line tools to access IPNI indexers and index-providers. Tools are in the form of separate executables for better composability with eachother and other command line commands.

## Install

```sh
go install github.com/ipni/ipni-cli/...@latest
```

This will install all the commands into `$GOPATH/bin/`

## Run

After installing, the following commands will be available:

- `advert` - Show information about an advertisement from a specified publisher
- `find` - Find value by CID or multihash in indexer
- `provider` - Show information about one or more providers known to an indexer
- `spaddr` - Get storage provider p2p ID and address from lotus gateway
- `verifyingest` - Verifies an indexer's ingestion of multihashes

To see instructions for use, run any individual cli application with the `--help` flag

## Build

If working within a clone of this repo, run `make` to build the all the executables and put them in the `bin` directory.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
