# ipni-cli
[![](https://img.shields.io/badge/made%20by-Protocol%20Labs-blue.svg?style=flat-square)](https://protocol.ai)
[![Go Reference](https://pkg.go.dev/badge/github.com/ipni/ipni-cli.svg)](https://pkg.go.dev/github.com/ipni/ipni-cli)

:computer: CLI tools for all things IPNI

This project provides a command line utility to access IPNI indexers and index-providers.

## Install

### Install binary

Get to the [latest ipni-cli release](https://github.com/ipni/ipni-cli/releases/latest) and get the binary appropriate for your system.

### Build with Golang

```sh
git clone https://github.com/ipni/ipni-cli.git
cd ipni-cli
go install ./cmd/ipni
```

This will install the `ipni` command into `$GOPATH/bin/`

## Run

To see instructions for use, run:
```
ipni --help
```

## Examples

Here are a few examples that use the following commands:
- `ads`       Show advertisements on a chain from a specified publisher
  - `get`         Show information about an advertisement from a specified publisher
  - `list`        List advertisements from latest to earlier from a specified publisher
  - `crawl`       Crawl publisher's advertisements and show information for each advertisement
  - `dist`        Determine the distance between two advertisements in a chain
- `find`      Find value by CID or multihash in indexer
- `provider`  Show information about providers known to an indexer
- `random`    Show random multihashes from a random advertisement
- `spaddr`    Get storage provider p2p ID and address from lotus gateway
- `verify`    Verifies advertised content validity and queryability from an indexer

### `ads get`
- Show the latest advertisement from a publisher:
```sh
ipni ads get --ai=/ip4/76.219.232.45/tcp/24001/p2p/12D3KooWPNbkEgjdBNeaCGpsgCrPRETe4uBZf1ShFXStobdN18ys --head
```
- Show information about a specific advertisement:
```
./ipni ads get --ai=/ip4/76.219.232.45/tcp/24001/p2p/12D3KooWPNbkEgjdBNeaCGpsgCrPRETe4uBZf1ShFXStobdN18ys \
    --cid=baguqeerank3iclae2u4lin3vj2avuory3ny67tldh2cd5uodsgsdl6uawz3a
```
- Show information about multiple advertisements:
```sh
ipni ads get --ai=/ip4/76.219.232.45/tcp/24001/p2p/12D3KooWPNbkEgjdBNeaCGpsgCrPRETe4uBZf1ShFXStobdN18ys \
    --cid=baguqeerank3iclae2u4lin3vj2avuory3ny67tldh2cd5uodsgsdl6uawz3a
    --cid=baguqeera3aylz3gkoxtkmqdwulxlaqbudf7nhdomfpyjqij236pwehrngngq
```
- Get ads from a list of CIDs in a file:
```sh
cat ad-cids-list.txt | ipni add get /dns4/ads.example.com/tcp/24001/p2p/<publisher-p2p-id>
```

### `ads list`
- List the 10 most recent advertisements from a provider:
```sh
ipni ads list -n 10 --ai=/ip4/38.70.220.112/tcp/10201/p2p/12D3KooWEAcRJ5fYjuavKgAhu79juR7mgaznSZxsm2RRUBiWurv9
```

### `ads crawl`
- Crawl advertisements from a provider and stop after reading 1000 multihashes:
```sh
ipni ads crawl -stop-mhs 1000 --ai=/ip4/38.70.220.112/tcp/10201/p2p/12D3KooWEAcRJ5fYjuavKgAhu79juR7mgaznSZxsm2RRUBiWurv9
```

### `ads dist`
- Get distance from an advertisement to the head of the advertisement chain:
```sh
ipni ads dist --ai=/ip4/76.219.232.45/tcp/24001/p2p/12D3KooWPNbkEgjdBNeaCGpsgCrPRETe4uBZf1ShFXStobdN18ys \
    --start=baguqeera3aylz3gkoxtkmqdwulxlaqbudf7nhdomfpyjqij236pwehrngngq
```
- Find the distance between 2 advertisements on a publisher's chain:
```sh
ipni ads dist --ai=/ip4/76.219.232.45/tcp/24001/p2p/12D3KooWPNbkEgjdBNeaCGpsgCrPRETe4uBZf1ShFXStobdN18ys \
    --start=baguqeera3aylz3gkoxtkmqdwulxlaqbudf7nhdomfpyjqij236pwehrngngq \
    --end=baguqeerage4rh6yqy4u37x7i337q57wrwfls5ihiei6l72rr6ezrw5vcucea
```

**Note* To include an HTTP path prefix in the `addr-info` flag of the `ads` command, include the `http-path` component in the multiaddr. For example, `--ai /dns/pool.example.com/https/http-path/eu%2Fprovider1/p2p/12D3KooWPMGfQs5CaJKG4yCxVWizWBRtB85gEUwiX2ekStvYvqgp` fetches ads from `https://pool.example.com/eu/provider1/ipni/v1/ad/head`. Any "/" within the http-path must be escaped.

### `find`
- Ask cid.contact where to find CID `bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy`:
```sh
ipni find -i https://cid.contact --cid bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy
```
- Ask cid.contact where to find multiple multihashes:
```sh
./ipni find -i https://cid.contact \
    --mh=2Drjgb5kxWdcTNfhfEC8F3Ltk4s16aAgG2aLnXxSdpiGTazLGE \
    --mh=2Drjgb4GmZ3cJGRunHYdHrmtgbmGoDuSMeN42gdU1jSiGmHVmA \
    --mh=2DrjgbJZxQgMTvWDG6ih2SNESWeoabccawmLwuFt1T59joGFxd
```

### `provider`
- Get all providers known by the indexer dev.cid.contact:
```
ipni provider -i https://dev.cid.contact --all
```
- Get information about the provider with ID `QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC`
```
ipni provider -i https://cid.contact -pid QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC
```
```
echo QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC | ipni provider -i https://cid.contact
```
- Get indexer's current ingestion distance, from the most recent published advertisement, for a provider:
```sh
ipni provider -i https://indexstar.prod.cid.contact -pid QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC --distance
```
- Get information about the providers returned from find results:
```sh
ipni find -i https://cid.contact --cid bafybeigvgzoolc3drupxhlevdp2ugqcrbcsqfmcek2zxiw5wctk3xjpjwy --id-only | ipni provider -i https://cid.contact
```
- See which providers cid.contact knows about that dev.cid.contact does not:
```sh 
ipni provider --all -i https://dev.cid.contact --id-only | ipni provider -invert -i https://cid.contact --id-only
```
- Get combined provider information from multiple indexers:
```
ipni provider --all -i https://alva.dev.cid.contact -i https://cora.dev.cid.contact --id-only | wc -l
```
- Watch indexer stay up-to-date with a provider's advertisement chain:
```
ipni provider -i https://inga.prod.cid.contact -pid QmQzqxhK82kAmKvARFZSkUVS6fo9sySaiogAnx5EnZ6ZmC -follow-dist -uin=10s
```

### `random`
- For specified providers, choose an advertisement with undeleted content from a random depth between 1 and n in the chain and return m random multihashs from the first entries block.
```
ipni random -i https://cid.contact --n=5 --m=3 --pid=12D3KooWC8QzjdzWynwYybjDLKa1YbPiRXUjwsibERubatgmQP51
```
- Lookup provider information for each random multihash.
```
ipni random --quiet -i https://cid.contact --n=5 --m=3 --pid=12D3KooWC8QzjdzWynwYybjDLKa1YbPiRXUjwsibERubatgmQP51 | xargs -J % -n1 ipni find -i https://cid.contact -mh %
```

### `spaddr`
- Get p2p ID and multiaddrs of storage provider identified by storage provider ID "t01000":
```
ipni spaddr --spid=t01000
```

### `verify ingest`
- Verfy ingestion at cid.contact, of multihashes 
```
ipni verify ingest -i https://cid.contact \
    --ad-cid=baguqeerank3iclae2u4lin3vj2avuory3ny67tldh2cd5uodsgsdl6uawz3a \
    --provider-id=12D3KooWPNbkEgjdBNeaCGpsgCrPRETe4uBZf1ShFXStobdN18ys \
    --batch-size=25 \
    --sampling-prob=0.125
```

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
