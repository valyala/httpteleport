package main

import (
	"flag"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/httpteleport"
	"github.com/valyala/tcplisten"
	"log"
	"net"
	"strings"
	"time"
)

var (
	reusePort   = flag.Bool("reusePort", false, "Whether to enable SO_REUSEPORT on -in if -inType is http or httptp")

	in  = flag.String("in", ":8080", "-inType addresses to listen to for incoming requests")
	inType = flag.String("inType", "http", "Type of -in address. Possible values:\n"+
		"\thttp - listen for HTTP requests over TCP, e.g. -in=127.0.0.1:8080\n"+
		"\tunix - listen for HTTP requests over unix socket, e.g. -in=/var/httptp/sock.unix\n"+
		"\thttptp - listen for httptp connections over TCP, e.g. -in=127.0.0.1:8043")
	inDelay = flag.Duration("inDelay", 0, "How long to wait before sending batched responses back if -inType=httptp")

	out  = flag.String("out", "127.0.0.1:8043", "Comma-separated list of -outType addresses to forward requests to.\n"+
		"Each request is forwarded to the least loaded address")
	outType = flag.String("outType", "httptp", "Type of -out address. Possible values:\n"+
		"\thttp - forward requests to HTTP servers on TCP, e.g. -out=127.0.0.1:80\n"+
		"\tunix - forward requests to HTTP servers on unix socket, e.g. -out=/var/nginx/sock.unix\n"+
		"\thttptp - forward requests to httptp servers over TCP, e.g. -out=127.0.0.1:8043")
	outDelay = flag.Duration("outDelay", 0, "How long to wait before forwarding incoming requests to -out if -outType=httptp")

	concurrency = flag.Int("concurrency", 100000, "The maximum number of concurrent requests httptp may process")
	timeout = flag.Duration("timeout", 3*time.Second, "The maximum duration for waiting response from -out server")
)

func main() {
	flag.Parse()

	outs := strings.Split(*out, ",")

	switch *outType {
	case "http":
		initHTTPClients(outs)
	case "unix":
		initUnixClients(outs)
	case "httptp":
		initHTTPTPClients(outs)
	default:
		log.Fatalf("unknown -outType=%q. Supported values are: http, unix, httptp")
	}

	switch *inType {
	case "http":
		serveHTTP()
	case "unix":
		serveUnix()
	case "httptp":
		serveHTTPTP()
	default:
		log.Fatalf("unknown -inType=%q. Supported values are: http, unix and httptp")
	}
}

func initHTTPClients(outs []string) {
	connsPerAddr := *concurrency / len(outs)
	for _, addr := range outs {
		c := &fasthttp.HostClient{
			Addr:     addr,
			MaxConns: connsPerAddr,
		}
		upstreamClients = append(upstreamClients, c)
	}
	log.Printf("Forwarding requests to HTTP servers at %q", outs)
}

func initUnixClients(outs []string) {
	connsPerAddr := *concurrency / len(outs)
	for _, addr := range outs {
		c := &fasthttp.HostClient{
			Addr:     addr,
			Dial: func(addr string) (net.Conn, error) {
				return net.Dial("unix", addr)
			},
			MaxConns: connsPerAddr,
		}
		upstreamClients = append(upstreamClients, c)
	}
	log.Printf("Forwarding requests to HTTP servers at unix:%q", outs)
}

func initHTTPTPClients(outs []string) {
	for _, addr := range outs {
		c := &httpteleport.Client{
			Addr:               addr,
			MaxBatchDelay:      *outDelay,
			MaxPendingRequests: *concurrency,
		}
		upstreamClients = append(upstreamClients, c)
	}
	log.Printf("Forwarding requests to httptp servers at %q", outs)
}

func serveHTTP() {
	ln := newTCPListener()
	s := newHTTPServer()

	log.Printf("listening for HTTP requests on %q", *in)
	if err := s.Serve(ln); err != nil {
		log.Fatalf("error in fasthttp server: %s", err)
	}
}

func serveUnix() {
	ln, err := net.Listen("unix", *in)
	if err != nil {
		log.Fatalf("cannot listen to -in=%q: %s", *in, err)
	}
	s := newHTTPServer()

	log.Printf("listening for HTTP requests on unix:%q", *in)
	if err := s.Serve(ln); err != nil {
		log.Fatalf("error in fasthttp server: %s", err)
	}
}

func serveHTTPTP() {
	ln := newTCPListener()
	s := httpteleport.Server{
		Handler:           httptpRequestHandler,
		Concurrency:       *concurrency,
		MaxBatchDelay:     *inDelay,
		ReduceMemoryUsage: true,
	}

	log.Printf("listening for httptp connections on %q", *in)
	if err := s.Serve(ln); err != nil {
		log.Fatalf("error in fasthttp server: %s", err)
	}
}

func newTCPListener() net.Listener {
	cfg := tcplisten.Config{
		ReusePort: *reusePort,
	}
	ln, err := cfg.NewListener("tcp4", *in)
	if err != nil {
		log.Fatalf("cannot listen to -in=%q: %s", *in, err)
	}
	return ln
}

func newHTTPServer() *fasthttp.Server {
	return &fasthttp.Server{
		Handler:     httpRequestHandler,
		Concurrency: *concurrency,
		ReduceMemoryUsage: true,
	}
}

type client interface {
	DoTimeout(req *fasthttp.Request, resp *fasthttp.Response, timeout time.Duration) error
	PendingRequests() int
}

var upstreamClients []client

func httpRequestHandler(ctx *fasthttp.RequestCtx) {
        var buf [16]byte
        ip := fasthttp.AppendIPv4(buf[:0], ctx.RemoteIP())
        ctx.Request.Header.SetBytesV("X-Forwarded-For", ip)

	c := leastLoadedClient()
	err := c.DoTimeout(&ctx.Request, &ctx.Response, *timeout)
	if err == nil {
		return
	}

	ctx.ResetBody()
	fmt.Fprintf(ctx, "HTTP proxying error: %s", err)
	if err == fasthttp.ErrTimeout {
		ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
	} else {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
	}
}

func httptpRequestHandler(ctx *fasthttp.RequestCtx) {
	c := leastLoadedClient()
	err := c.DoTimeout(&ctx.Request, &ctx.Response, *timeout)
	if err == nil {
		return
	}

	ctx.ResetBody()
	fmt.Fprintf(ctx, "httptp proxying error: %s", err)
	if err == httpteleport.ErrTimeout {
		ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
	} else {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
	}
}

func leastLoadedClient() client {
	minC := upstreamClients[0]
	minN := minC.PendingRequests()
	if minN == 0 {
		return minC
	}
	for _, c := range upstreamClients[1:] {
		n := c.PendingRequests()
		if n == 0 {
			return c
		}
		if n < minN {
			minC = c
			minN = n
		}
	}
	return minC
}
