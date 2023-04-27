# ipni-cli
[![](https://img.shields.io/badge/made%20by-Protocol%20Labs-blue.svg?style=flat-square)](https://protocol.ai)
[![Go Reference](https://pkg.go.dev/badge/github.com/ipni/ipni-cli.svg)](https://pkg.go.dev/github.com/ipni/ipni-cli)
[![Go Test](https://github.com/ipni/ipni-cli/actions/workflows/go-test.yml/badge.svg)](https://github.com/ipni/ipni-cli/actions/workflows/go-test.yml)

:computer: CLI tool for all things IPNI

This project provides a command-line interface (CLI), in Golang, to access IPNI indexers and index-providers. It also provides a `command` package that can be imported into other Golang projects to implement the same commands as provided in the CLI.

## Install

```sh
go install github.com/ipni/ipni-cli@latest
```

## Run

To see instructions for use, run the following:
```sh
ipni-cli --help
```

## License
[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
