============
httpteleport

Teleports 10Gbps http traffic over 1Gbps networks.

# Use cases

- RTB servers
- API servers
- Reverse proxies
- Load balancing


# How it works

It just sends batched http requests and responses over a single compressed
connection. This solves the following issues:

- High network bandwidth usage
- High network packets rate
- A lot of open TCP connections


# Links

[Docs](https://godoc.org/github.com/valyala/httpteleport)

[httptp](https://github.com/valyala/httpteleport/tree/master/cmd/httptp) - reverse proxy and
load balancer based on httpteleport.
