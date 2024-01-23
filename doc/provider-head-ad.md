## Get the Head Advertisement for Providers

The get the head advertisement for every provider:

```shell
ipni provider --all --i=https://cid.contact --publisher | xargs -I addrinfo -R 1 ipni ads get --head --ai addrinfo
```

To get the head advertisement for a single provider:

```shell
ipni provider --pid=12D3KooWC8QzjdzWynwYybjDLKa1YbPiRXUjwsibERubatgmQP51 --i=https://cid.contact --publisher | xargs ipni ads get --head --ai
```

