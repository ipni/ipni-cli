## Lookup Random Multihash from Random Provider

You can use the `random` command from version v0.1.4 of the ipni-cli to do lookup a random multihash.

Here is a complete command that selects a random provider, random advertisement, and random multihash, and then does a lookup of provider info for the multihash:
```shell
ipni provider -i https://cid.contact --all --error --invert --id-only | xargs shuf -n1 -e | ipni random -i https://cid.contact --n=5 --m=1 --quiet | xargs -J % -n1 ipni find -i https://cid.contact -mh %
```
Let's break that down...

The first part,
```shell
ipni provider -i https://cid.contact --all --error --invert --id-only 
```
outputs the provider ID (`--id-only`) of all providers (`--all`) that do not have a LastError (`--error --invert`).

The second part,
```shell
xargs shuf -n1 -e
```
selects 1 of the provider IDs given as input end echos that to output

The third part,
```shell
ipni random -i https://cid.contact --n=5 --m=1 --quiet
```
selects a random suitable* advertisement from the provider (ID from stdin) from the latest 5 ads (`--n=5`) and then selects 1 (`--m=1`) random multihash from the first entries block, and outputs only that multihash (`--quiet`).

* suitable means the advertisement is not a removal and has content that has not been removed by a removal advertisement later in the ad chain.

The fourth part,
```shell
xargs -J % -n1 ipni find -i https://cid.contact -mh %
```
uses `ipni find` to lookup the provider info for the random multihash
