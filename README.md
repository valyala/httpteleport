[![Build Status](https://travis-ci.org/valyala/httpteleport.svg)](https://travis-ci.org/valyala/httpteleport)
[![GoDoc](https://godoc.org/github.com/valyala/httpteleport?status.svg)](http://godoc.org/github.com/valyala/httpteleport)
[![Go Report](https://goreportcard.com/badge/github.com/valyala/httpteleport)](https://goreportcard.com/report/github.com/valyala/httpteleport)

# httpteleport

Teleports 10Gbps http traffic over 1Gbps networks.
Built on top of [fastrpc](https://github.com/valyala/fastrpc).


# Use cases

`httpteleport` may significantly reduce inter-server network bandwidth overhead
and costs for the following cases:

- RTB servers.
- HTTP-based API servers (aka REST, JSON, JSON-RPC or HTTP-RPC services
  and microservices).
- Reverse proxies.
- Load balancers.


# How does it work?

It just sends batched http requests and responses over a single compressed
connection. This solves the following issues:

- High network bandwidth usage
- High network packets rate
- A lot of open TCP connections


Unlike [http pipelining](https://en.wikipedia.org/wiki/HTTP_pipelining),
`httpteleport` responses may be sent out-of-order.
This resolves [head of line blocking](https://en.wikipedia.org/wiki/Head-of-line_blocking) issue.


# Links

* [Docs](https://godoc.org/github.com/valyala/httpteleport)

* [httptp](https://github.com/valyala/httpteleport/tree/master/cmd/httptp) -
  standalone single-binary reverse proxy and load balancer based
  on `httpteleport`. `httptp` source code may be used as an example
  of `httpteleport` usage.


# FAQ

* Q: Why `httpteleport` doesn't use [HTTP/2.0](https://en.wikipedia.org/wiki/HTTP/2)?

  A: Because `http/2.0` has many features, which aren't used by `httpteleport`.
     More features complicate the code, make it more error-prone and may slow
     it down.

* Q: Why does `httpteleport` provide [fasthttp](https://github.com/valyala/fasthttp)-
     based API instead of standard [net/http](https://golang.org/pkg/net/http/)-
     based API?

  A: Because `httpteleport` is optimized for speed. So it have to use `fasthttp`
     for http-related stuff to be fast.

* Q: Give me performance numbers.

  A: `httpteleport` achieves 200K qps on a single CPU core in end-to-end test,
     where a client sends requests to a local server and the server sends
     responses back to the client:

  ```
GOMAXPROCS=1 go test -bench=. -benchmem
goos: linux
goarch: amd64
pkg: github.com/valyala/httpteleport
BenchmarkEndToEndGetNoDelay1          	  300000	      4346 ns/op	  60.05 MB/s	       0 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay10         	  300000	      4370 ns/op	  59.71 MB/s	       3 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay100        	  300000	      4406 ns/op	  59.23 MB/s	       6 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay1000       	  300000	      4457 ns/op	  58.55 MB/s	      24 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay10K        	  300000	      5868 ns/op	  44.48 MB/s	     178 B/op	       1 allocs/op
BenchmarkEndToEndGetDelay1ms          	  300000	      4771 ns/op	  54.70 MB/s	      21 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay2ms          	  200000	      7943 ns/op	  32.86 MB/s	      31 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay4ms          	  200000	      7741 ns/op	  33.71 MB/s	      31 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay8ms          	  200000	     10580 ns/op	  24.67 MB/s	      26 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay16ms         	  100000	     16923 ns/op	  15.42 MB/s	      50 B/op	       0 allocs/op
BenchmarkEndToEndGetCompressNone      	  200000	      7899 ns/op	  33.04 MB/s	      31 B/op	       0 allocs/op
BenchmarkEndToEndGetCompressFlate     	  100000	     13257 ns/op	  19.69 MB/s	     129 B/op	       0 allocs/op
BenchmarkEndToEndGetCompressSnappy    	  200000	      8158 ns/op	  31.99 MB/s	      40 B/op	       0 allocs/op
BenchmarkEndToEndGetTLSCompressNone   	  200000	      8692 ns/op	  30.02 MB/s	      39 B/op	       0 allocs/op
BenchmarkEndToEndGetTLSCompressFlate  	  100000	     13710 ns/op	  19.04 MB/s	     131 B/op	       0 allocs/op
BenchmarkEndToEndGetTLSCompressSnappy 	  200000	      8480 ns/op	  30.78 MB/s	      42 B/op	       0 allocs/op
BenchmarkEndToEndGetPipeline1         	  300000	      4673 ns/op	  55.85 MB/s	       0 B/op	       0 allocs/op
BenchmarkEndToEndGetPipeline10        	  300000	      4610 ns/op	  56.61 MB/s	       3 B/op	       0 allocs/op
BenchmarkEndToEndGetPipeline100       	  300000	      4576 ns/op	  57.03 MB/s	       6 B/op	       0 allocs/op
BenchmarkEndToEndGetPipeline1000      	  300000	      4886 ns/op	  53.41 MB/s	      26 B/op	       0 allocs/op
```
