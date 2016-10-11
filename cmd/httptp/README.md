======
httptp

`httptp` is an [httpteleport](https://github.com/valyala/httpteleport) proxy
and load balancer, which accepts incoming requests at -in address and forwards
them to -out addresses. Each request is forwarded to the least loaded
-out address.


# Features

  * Fast. It is based on [fasthttp](https://github.com/valyala/fasthttp).
    Easily handles 100K qps and over.

  * May reduce required network bandwidth between servers by up to 10x. I.e.:

    * 10Gbit HTTP traffic may be sent over 1Gbit link.
    * 1Gbit HTTP traffic may be sent over 100Mbit link.
    * 100Mbit HTTP traffic may be sent over 10Mbit link.

    This may have the following benefits:

    * Save a lot of money for expensive inter-datacenter traffic.
    * Free internal network bandwidth for other services.

  * Supports encrypted connections.

  * May substitute nginx in reverse proxy mode, load balancer mode and
    TLS offloading mode.

  * May limit the maximum number of open connections per upstream host.

  * May accept and/or forward http requests from/to unix sockets.


# Usage

```
go get -u github.com/valyala/httpteleport/cmd/httptp
httptp -help
```

# Usage examples

Accept incoming HTTP requests at port 8042 and teleport them to the given
httptp servers:
```
httptp -inType=http -in=:8042 -outType=httptp -out=httptp-server1:8043,httptp-server2:8043
```

Accept teleported requests at port 8043 and proxy them to the given HTTP servers:
```
httptp -inType=httptp -in=:8043 -outType=http -out=http-server1:80,http-server2:80
```

Accept teleported requests at port 8043 and teleport them to the given httptp
servers:
```
httptp -inType=httptp -in=:8043 -outType=httptp -out=httptp-server1:8043,httptp-server2:8043
```

Accept incoming HTTP requests at port 8080 and proxy them to the given
HTTP servers:
```
httptp -inType=http -in=:8080 -outType=http -out=http-server1:80,http-server2:80
```

Accept incoming HTTP requests at unix socket and teleport them to httptp server:
```
httptp -inType=unix -in=/var/httptp/sock.unix -outType=httptp -out=httptp-server:8043
```

Accept teleported requests at port 8043 and proxy them to the given unix socket:
```
httptp -inType=httptp -in=:8043 -outType=unix -out=/var/nginx/sock.unix
```
