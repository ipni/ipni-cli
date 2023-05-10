# ipni-cli
[![](https://img.shields.io/badge/made%20by-Protocol%20Labs-blue.svg?style=flat-square)](https://protocol.ai)
[![Go Reference](https://pkg.go.dev/badge/github.com/ipni/ipni-cli.svg)](https://pkg.go.dev/github.com/ipni/ipni-cli)

:computer: CLI tools for all things IPNI

This project provides a command line utility to access IPNI indexers and index-providers.

## Install

```sh
go install github.com/ipni/ipni-cli/cmd/ipni@latest
```

This will install the `ipni` command into `$GOPATH/bin/`

## Run

To see instructions for use, run:
```
ipni --help
```

## Examples

Show the latest advertisement from a publisher:
```sh
ipni advert --head /dns4/ipni-ads.example.com/tcp/443/https/p2p/12D3KooWQ9j3Ur5V9U63Vi6ved72TcA3sv34k74W3wpW5rwNvDc3
```

Ask cid.contact where to find CID `bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy`:
```sh
ipni find -i cid.contact --cid bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy
```

Get information about the provider with ID `QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC`
```
ipni provider -i cid.contact -pid QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC
```
```
echo QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC | ipni provider -i cid.contact
```

Get information about the providers returned from find results:
```sh
ipni find -i cid.contact --cid bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy --id-only | ipni provider -i cid.contact
```

See which providers cid.contact knows about that dev.cid.contact does not:
```sh 
ipni provider --all -i dev.cid.contact -id | ipni provider -invert -i cid.contact -id
```

To get ads from a list in a file:
```sh
cat ad-cids-list.txt | ipni advert /dns4/ads.example.com/tcp/24001/p2p/12D3KooWLjeDyvuv7rbfG2wWNvWn7ybmmU88PirmSckuqCgXBAph
```

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
