# httptp

`httptp` is an http(s) proxy and load balancer that saves network bandwidth.
It is built on top of [httpteleport](https://github.com/valyala/httpteleport).
It accepts incoming requests at `-in` address and forwards them to `-out` addresses.
Each request is forwarded to the least loaded healthy `-out` address.

Any highly loaded http-based API service and microservice may benefit from
`httptp` usage. Here are a few buzzwords related to such services:

  * [RTB](https://en.wikipedia.org/wiki/Real-time_bidding)
  * [REST](https://en.wikipedia.org/wiki/Representational_state_transfer)
  * [JSON-RPC](https://en.wikipedia.org/wiki/JSON-RPC)
  * [XML-RPC](https://en.wikipedia.org/wiki/XML-RPC)


# Features

  * Easy to use and configure - just run a single `httptp` binary with required
    command-line options.

  * Fast. It is based on [fasthttp](https://github.com/valyala/fasthttp).
    Easily handles more than 100K qps.

  * May reduce required network bandwidth between servers by up to 10x. I.e.:

    * 10Gbit HTTP traffic may be sent over 1Gbit link.
    * 1Gbit HTTP traffic may be sent over 100Mbit link.
    * 100Mbit HTTP traffic may be sent over 10Mbit link.

    This may have the following benefits:

    * Save a lot of money for expensive inter-datacenter traffic.
    * Free internal network bandwidth for other services.

  * Supports encrypted connections on both `-in` and `-out` ends.

  * [HTTP keep-alive connections](https://en.wikipedia.org/wiki/HTTP_persistent_connection)
    are used by default on both `-in` and `-out` ends. `httptp` easily handles
    more than 100K of incoming concurrent keep-alive connections.

  * May substitute `nginx` in [reverse proxy](https://en.wikipedia.org/wiki/Reverse_proxy)
    mode, [load balancer](https://en.wikipedia.org/wiki/Load_balancing_(computing)) mode and
    [TLS offloading](https://www.nginx.com/blog/nginx-ssl/) mode.

  * Automatically adjusts load to upstream servers:

    * Faster servers receive more requests.
    * Slower and unhealthy servers receive less requests.

  * May limit the maximum number of open connections per upstream host.

  * May accept and/or forward http requests from/to unix sockets.

  * Collects and exports various stats at /expvar page.

  * Easy to extend and customize. `httptp` is open source software written
    in [Go](https://golang.org/) - easy to read and hack language.
    It is released under [MIT license](https://github.com/valyala/httpteleport/blob/master/cmd/httptp/LICENSE),
    so it may be easily customized and extended.


# Usage

```
go get -u github.com/valyala/httpteleport/cmd/httptp
httptp -help
```

# Examples


## Reducing network bandwidth between datacenters

Suppose you have an RTB partner sending you 50K requests per second.
Each RTB request contains ~2Kb JSON body according to [RTB spec](https://www.iab.com/guidelines/real-time-bidding-rtb-project/).
50K * 2Kb * 8bits = 0.8Gbps network bandwidth is required between
the partner and you. In reality the required network bandwidth exceeds
1Gbps due to network protocols overhead. This may be quite expensive
if your servers and partner servers are located in distinct datacenters.
This also may be limiting factor for growth.

Let's decrease the required network bandwidth and the corresponding expenses
by 10x with `httptp`!

Suppose you have three worker servers hidden behind `nginx` running
on the ip `69.69.69.69`:

```nginx
upstream rtb {
	rtb-server1:80;
	rtb-server2:80;
	rtb-server3:80;

	keepalive 100000;
}

server {
	listen 69.69.69.69:80;
	location / {
		proxy_pass http://rtb;
		proxy_http_version 1.1;
		proxy_set_header Connection "";
	}
}
```

Then start `httptp` on port `9876` on the same machine:

```
httptp -inType=teleport -in=69.69.69.69:9876 -outType=http -out=rtb-server1:80,rtb-server2:80,rtb-server3:80
```

Ask your partner starting `httptp` in his local network for proxying
RTB traffic to you:

```
# let's assume httptp is started at the server with local ip 10.10.10.10
httptp -inType=http -in=10.10.10.10:6789 -outType=teleport -out=69.69.69.69:9876
```

Then the partner may send rtb traffic to `10.10.10.10:6789` in his local network.
This traffic will be compressed and proxied to `httptp` listening
`69.69.69.69:9876` in your network. The `httptp` will spread the traffic across
your worker servers set in the `-out` parameter: `rtb-server1:80,rtb-server2:80,rtb-server3:80`.

The result: network traffic between you and the partner is decreased by 10x.
So the partner may send 10x more RTB requests to you. This may allow you and
your partner earning more money :)


## Reducing network bandwidth in local networks

The previous example decreased inter-datacenter network traffic.
But the amount of local traffic between `httptp` and worker servers didn't
change. `httptp` may solve the issue - just start `httptp`
on each worker server:

```
httptp -inType=teleport -in=:8345 -outType=http -out=127.0.0.1:80
```

Then restart `httptp` on proxy server, so it would route traffic to just started
`httptp` instances on each worker server:

```
httptp -inType=teleport -in=69.69.69.69:9876 -outType=teleport -out=rtb-server1:8345,rtb-server2:8345,rtb-server3:8345
```

Great! What about the partner? It still requires a lot of internal network
bandwidth between his servers and `httptp` running at `10.10.10.10:6789`
in his local network.

This issue is easily solved - just run `httptp` on each of the server,
so it bypasses the local `httptp` at `10.10.10.10:6789` and routes
the traffic directly to our `httptp` at `69.69.69.69:9876`:

```
httptp -inType=http -in=127.0.0.1:5438 -outType=teleport -out=69.69.69.69:9876
```

Don't forget modifying destination address from `10.10.10.10:6789`
to `127.0.0.1:5438` on each of the servers.


## Optimizing local inter-process communications

`httptp` in the previous example routes traffic to a locally running RTB service
via `127.0.0.1`. This isn't the fastest approach - [unix sockets](https://en.wikipedia.org/wiki/Unix_domain_socket)
are usually faster. Luckily `httptp` supports `unix sockets` out of the box.
Just run it on each of the worker server with the following options:

```
httptp -inType=teleport -in=:8345 -outType=unix -out=/path/to/rtb/unix.socket
```

RTB servers must be able to accept http traffic from local unix socket
`/path/to/rtb/unix.socket`.


The same optimization applies to partner side:

```
httptp -inType=unix -in=/path/to/httptp/unix.socket -outType=teleport -out=69.69.69.69:9876
```

RTB servers must route http traffic to local unix socket `/path/to/httptp/unix.socket`.


## Traffic encryption

In the previous examples RTB traffic is passed unencrypted over public networks
when traveling between you and the partner. Luckily `httptp` supports
[TLS encryption](https://en.wikipedia.org/wiki/Transport_Layer_Security)
out of the box - just use `teleports` traffic type instead of `teleport`.

Run `httptp` with the following options on your proxy server:

```
httptp -inType=teleports -inTLSCert=/path/to/tls.cert -inTLSKey=/path/to/tls.key \
	-in=69.69.69.69:4443 -outType=teleport -out=rtb-server1:8345,rtb-server2:8345,rtb-server3:8345
```

Note that you must have valid TLS certificate and key files for valid domain
name pointing to ip `69.69.69.69`. Path to TLS ceritificate file is passed
via `-inTLSCert`, path to TLS key file is passed via `-inTLSKey`.

And ask your partner restarting `httptp` on each server with the following options:

```
httptp -inType=unix -in=/path/to/httptp/unix.socket -outType=teleports -out=domain-name-for.ip.69.69.69.69:4443
```

Where `domain-name-for.ip.69.69.69.69` is a domain name from your certificate.


## Batching

By default `httptp` forwards requests and responses immediately. This means that
each request or response results in at least one network packet.
Each network packet isn't free:

  * It consumes additional CPU time.
  * It consumes additional network resources.
  * It contains a [header overhead](http://stackoverflow.com/questions/24879959/what-is-overhead-payload-and-header),
    which may be quite big comparing to the request / response size.
  * It may hurt compression - multiple requests / responses are usually
    compressed better than a single request / response.

`httptp` allows sending multiple requests / responses in a single packet.
This is called `batching`. Just set non-zero `-inDelay` and/or `-outDelay`
when starting `httptp`.

Beware of the following batching issues:

  * Batching may introduce delays.
  * Batching may be useful only for high load, i.e. at least thousands
    of requests per second. Otherwise it is useless.


## Compression

By default `httptp` compresses both requests and responses. While compression
saves network bandwidth, it isn't free - it consumes an additional CPU time.

`httptp` supports the following compression levels independently for requests
and responses via `-inCompress` and `-outCompress` options:

  * none - compression is disabled
  * [flate](https://en.wikipedia.org/wiki/DEFLATE) - default compression
  * [snappy](https://en.wikipedia.org/wiki/Snappy_(compression)) - lightweight compression


## Restricting access to `httptp`

`httptp` running on your proxy node `69.69.69.69` in examples above accepts
incoming connections from any IP address. I.e. anybody across the internet
may send requests to it. `httptp` supports restricting access only
to the given IP list - just pass allowed IPs to `-inAllowIP` option:

```
httptp -inType=teleport -in=69.69.69.69:9876 -inAllowIP=partner-ip1,partner-ip2 \
	-outType=teleport -out=rtb-server1:8345,rtb-server2:8345,rtb-server3:8345
```


## HTTP load balancing

The following command starts `httptp` accepting http requests at port 80
and forwarding them to three worker nodes listed in the `-out` option:

```
httptp -inType=http -in=:80 -outType=http -out=node1:8080,node2:8080,node3:8080
```


## TLS offloading

The following command starts `httptp` accepting https requests at port 443
and forwarding them unencrypted to the given `-out` worker nodes:

```
httptp -inType=https -inTLSCert=/path/to/tls.cert -inTLSKey=/path/to/tls.key \
	-in=:443 -outType=http -out=node1:8080,node2:8080,node3:8080
```

## Advanced usage

`httptp` features may be integrated directly into your services.
Just use [httpteleport package](https://godoc.org/github.com/valyala/httpteleport)
in your clients and/or applications.
This will eliminate `httptp` hops from the path
`client <-> httptp <-> network <-> httptp <-> your application`,
saving network and CPU resources.

`httptp` contains other configuration options for advanced usage.
See `httptp -help` for more details:

```
$ httptp -help
Usage of ./httptp:
  -clientIPHeader string
    	HTTP request header for sending the original client ip.
	For instance, -clientIPHeader=X-Forwarded-For. Empty -clientIPHeader disables sending client ip in request headers
  -concurrency int
    	The maximum number of concurrent requests httptp may process.
	This also limits the maximum number of open connections per -out address if -outType=http or https (default 100000)
  -expvarAddr string
    	TCP address for exporting httptp metrics. They are exported at http://expvarAddr/expvar page (default "localhost:8040")
  -in string
    	-inType address to listen to for incoming requests (default "127.0.0.1:8080")
  -inAllowIP string
    	Comma-separated list of IP addresses allowed for establishing connections to -in.
	All IP addresses are allowed if empty
  -inCompress string
    	Which compression to use for responses if -inType=teleport.
	Supported values:
	none - responses aren't compressed. Low CPU usage at the cost of high network bandwidth
	flate - responses are compressed using flate algorithm. Low network bandwidth at the cost of high CPU usage
	snappy - responses are compressed using snappy algorithm. Balance between network bandwidth and CPU usage (default "flate")
  -inDelay duration
    	How long to wait before sending batched responses back if -inType=teleport
  -inTLSCert string
    	Path to TLS certificate file if -inType=https or teleports (default "/etc/ssl/certs/ssl-cert-snakeoil.pem")
  -inTLSKey string
    	Path to TLS key file if -inType=https or teleports (default "/etc/ssl/private/ssl-cert-snakeoil.key")
  -inTLSSessionTicketKey string
    	TLS sesssion ticket key if -inType=https or teleports. Automatically generated if empty.
	See https://blog.cloudflare.com/tls-session-resumption-full-speed-and-secure/ for details
  -inType string
    	Type of -in address. Supported values:
	http - accept http requests over TCP, e.g. -in=127.0.0.1:8080
	https - accept https requests over TCP, e.g. -in=127.0.0.1:443
	unix - accept http requests over unix socket, e.g. -in=/var/httptp/sock.unix
	teleport - accept httpteleport connections over TCP, e.g. -in=127.0.0.1:8043
	teleports - accept httpteleport connections over encrypted TCP, e.g. -in=127.0.0.1:8443 (default "http")
  -out string
    	Comma-separated list of -outType addresses to forward requests to.
	Each request is forwarded to the least loaded address (default "127.0.0.1:8043")
  -outCompress string
    	Which compression to use for requests if -outType=teleport.
	Supported values:
	none - requests aren't compressed. Low CPU usage at the cost of high network bandwidth
	flate - requests are compressed using flate algorithm. Low network bandwidth at the cost of high CPU usage
	snappy - requests are compressed using snappy algorithm. Balance between network bandwidth and CPU usage (default "flate")
  -outConnsPerAddr int
    	How many connections must be established per each -out server if -outType=teleport.
	Usually a single connection is enough. Increase this value if the compression
	on the connection occupies 100% of a single CPU core.
	Alternatively, -inCompress and/or -outCompress may be set to snappy or none in order to reduce CPU load (default 1)
  -outDelay duration
    	How long to wait before forwarding incoming requests to -out if -outType=teleport
  -outTimeout duration
    	The maximum duration for waiting responses from -out server (default 3s)
  -outType string
    	Type of -out address. Supported values:
	http - forward requests to http servers on TCP, e.g. -out=127.0.0.1:80
	https - forward requests to https servers on TCP, e.g -out=127.0.0.1:443
	unix - forward requests to http servers on unix socket, e.g. -out=/var/nginx/sock.unix
	teleport - forward requests to httpteleport servers over TCP, e.g. -out=127.0.0.1:8043
	tepelorts - forward requests to httpteleport servers over encrypted TCP, e.g. -out=127.0.0.1:8043.
		Server must properly set -inTLS* flags in order to accept encrypted TCP connections (default "teleport")
  -reusePort
    	Whether to enable SO_REUSEPORT on -in if -inType is http or teleport
```
