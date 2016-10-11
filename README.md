============
httpteleport

Teleports 10Gbps http traffic over 1Gbps networks.


# Use cases

`httpteleport` may significantly reduce inter-server network bandwidth overhead
and costs for the following cases:

- RTB servers.
- HTTP-based API servers (aka REST and JSON services and microservices).
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
  standalone single-binary single-binary reverse proxy and load balancer
  based on `httpteleport`.


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
  A: `httpteleport` achieves 200K qps on a single CPU core:

  ```
$ GOMAXPROCS=1 go test -bench=. -benchmem -benchtime=10s
BenchmarkEndToEndGetNoDelay1       	 3000000	      4635 ns/op	  56.30 MB/s	       0 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay10      	 3000000	      4630 ns/op	  56.37 MB/s	       0 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay100     	 3000000	      4657 ns/op	  56.04 MB/s	       0 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay1000    	 3000000	      4777 ns/op	  54.64 MB/s	       2 B/op	       0 allocs/op
BenchmarkEndToEndGetNoDelay10K     	 2000000	      6613 ns/op	  39.46 MB/s	      26 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay1ms       	 3000000	      5822 ns/op	  44.82 MB/s	       2 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay2ms       	 2000000	      6677 ns/op	  39.09 MB/s	       3 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay4ms       	 2000000	      8820 ns/op	  29.59 MB/s	       3 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay8ms       	 1000000	     12978 ns/op	  20.11 MB/s	       6 B/op	       0 allocs/op
BenchmarkEndToEndGetDelay16ms      	 1000000	     20461 ns/op	  12.76 MB/s	       6 B/op	       0 allocs/op
BenchmarkEndToEndGetCompressNone   	 3000000	      5809 ns/op	  44.93 MB/s	       2 B/op	       0 allocs/op
BenchmarkEndToEndGetCompressFlate  	 1000000	     10608 ns/op	  24.60 MB/s	      12 B/op	       0 allocs/op
BenchmarkEndToEndGetCompressSnappy 	 2000000	      6252 ns/op	  41.75 MB/s	       3 B/op	       0 allocs/op
```
