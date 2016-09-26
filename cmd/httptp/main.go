package main

import (
	"expvar"
	"flag"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/httpteleport"
	"github.com/valyala/tcplisten"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

var (
	reusePort = flag.Bool("reusePort", false, "Whether to enable SO_REUSEPORT on -in if -inType is http or httptp")

	in     = flag.String("in", ":8080", "-inType address to listen to for incoming requests")
	inType = flag.String("inType", "http", "Type of -in address. Supported values:\n"+
		"\thttp - listen for HTTP requests over TCP, e.g. -in=127.0.0.1:8080\n"+
		"\tunix - listen for HTTP requests over unix socket, e.g. -in=/var/httptp/sock.unix\n"+
		"\thttptp - listen for httptp connections over TCP, e.g. -in=127.0.0.1:8043")
	inDelay    = flag.Duration("inDelay", 0, "How long to wait before sending batched responses back if -inType=httptp")
	inCompress = flag.String("inCompress", "flate", "Which compression to use for responses if -inType=httptp. "+
		"Supported values:\n"+
		"\tnone - responses aren't compressed. Low CPU usage at the cost of high network bandwidth\n"+
		"\tflate - responses are compressed using flate algorithm. Low network bandwidth at the cost of high CPU usage\n"+
		"\tsnappy - responses are compressed using snappy algorithm. Balance between network bandwidth and CPU usage")

	out = flag.String("out", "127.0.0.1:8043", "Comma-separated list of -outType addresses to forward requests to.\n"+
		"Each request is forwarded to the least loaded address")
	outType = flag.String("outType", "httptp", "Type of -out address. Supported values:\n"+
		"\thttp - forward requests to HTTP servers on TCP, e.g. -out=127.0.0.1:80\n"+
		"\tunix - forward requests to HTTP servers on unix socket, e.g. -out=/var/nginx/sock.unix\n"+
		"\thttptp - forward requests to httptp servers over TCP, e.g. -out=127.0.0.1:8043")
	outDelay    = flag.Duration("outDelay", 0, "How long to wait before forwarding incoming requests to -out if -outType=httptp")
	outCompress = flag.String("outCompress", "flate", "Which compression to use for requests if -outType=httptp. "+
		"Supported values:\n"+
		"\tnone - requests aren't compressed. Low CPU usage at the cost of high network bandwidth\n"+
		"\tflate - requests are compressed using flate algorithm. Low network bandwidth at the cost of high CPU usage\n"+
		"\tsnappy - requests are compressed using snappy algorithm. Balance between network bandwidth and CPU usage")

	outConnsPerAddr = flag.Int("outConnsPerAddr", 1, "How many connections must be established per each -out server if -outType=httptp.\n"+
		"\tUsually a single connection is enough. Increase this value if the compression\n"+
		"\ton the connection occupies 100% of a single CPU core.\n"+
		"\tAlternatively, -inCompress and/or -outCompress may be set to snappy or none in order to reduce CPU load")

	concurrency   = flag.Int("concurrency", 100000, "The maximum number of concurrent requests httptp may process")
	timeout       = flag.Duration("timeout", 3*time.Second, "The maximum duration for waiting responses from -out server")
	xForwardedFor = flag.Bool("xForwardedFor", false, "Whether to set client's ip in X-Forwarded-For request header for outgoing requests")
)

func main() {
	flag.Parse()

	initExpvarServer()

	outs := strings.Split(*out, ",")

	switch *outType {
	case "http":
		initHTTPClients(outs)
	case "unix":
		initUnixClients(outs)
	case "httptp":
		initHTTPTPClients(outs)
	default:
		log.Fatalf("unknown -outType=%q. Supported values are: http, unix, httptp", *outType)
	}

	switch *inType {
	case "http":
		serveHTTP()
	case "unix":
		serveUnix()
	case "httptp":
		serveHTTPTP()
	default:
		log.Fatalf("unknown -inType=%q. Supported values are: http, unix and httptp", *inType)
	}
}

func initHTTPClients(outs []string) {
	connsPerAddr := (*concurrency + len(outs) - 1) / len(outs)
	for _, addr := range outs {
		c := newHTTPClient(fasthttp.Dial, addr, connsPerAddr)
		upstreamClients = append(upstreamClients, c)
	}
	log.Printf("forwarding requests to HTTP servers at %q", outs)
}

func initUnixClients(outs []string) {
	connsPerAddr := (*concurrency + len(outs) - 1) / len(outs)
	for _, addr := range outs {
		verifyUnixAddr(addr)
		c := newHTTPClient(dialUnix, addr, connsPerAddr)
		upstreamClients = append(upstreamClients, c)
	}
	log.Printf("forwarding requests to HTTP servers at unix:%q", outs)
}

func verifyUnixAddr(addr string) {
	fi, err := os.Stat(addr)
	if err != nil {
		log.Fatalf("error when accessing unix:%q: %s", addr, err)
	}
	mode := fi.Mode()
	if (mode & os.ModeSocket) == 0 {
		log.Fatalf("the %q must be unix socket", addr)
	}
}

func initHTTPTPClients(outs []string) {
	concurrencyPerAddr := (*concurrency + len(outs) - 1) / len(outs)
	concurrencyPerAddr = (concurrencyPerAddr + *outConnsPerAddr - 1) / *outConnsPerAddr
	outCompressType := compressType(*outCompress, "outCompress")
	var cs []client
	for _, addr := range outs {
		c := &httpteleport.Client{
			Addr:               addr,
			MaxBatchDelay:      *outDelay,
			MaxPendingRequests: concurrencyPerAddr,
			ReadTimeout:        120 * time.Second,
			WriteTimeout:       5 * time.Second,
			CompressType:       outCompressType,
		}
		cs = append(cs, c)
	}
	for i := 0; i < *outConnsPerAddr; i++ {
		upstreamClients = append(upstreamClients, cs...)
	}
	log.Printf("forwarding requests to httptp servers at %q", outs)
}

func compressType(ct, name string) httpteleport.CompressType {
	switch ct {
	case "none":
		return httpteleport.CompressNone
	case "flate":
		return httpteleport.CompressFlate
	case "snappy":
		return httpteleport.CompressSnappy
	default:
		log.Fatalf("unknown -%s: %q. Supported values: none, flate, snappy", name, ct)
	}
	panic("unreached")
}

func newHTTPClient(dial fasthttp.DialFunc, addr string, connsPerAddr int) client {
	return &fasthttp.HostClient{
		Addr:         addr,
		Dial:         dial,
		MaxConns:     connsPerAddr,
		ReadTimeout:  *timeout * 5,
		WriteTimeout: *timeout,
	}
}

func dialUnix(addr string) (net.Conn, error) {
	return net.Dial("unix", addr)
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
	addr := *in
	if _, err := os.Stat(addr); err == nil {
		verifyUnixAddr(addr)
		if err := os.Remove(addr); err != nil {
			log.Fatalf("cannot remove %q: %s", addr, err)
		}
	}

	ln, err := net.Listen("unix", addr)
	if err != nil {
		log.Fatalf("cannot listen to -in=%q: %s", addr, err)
	}
	s := newHTTPServer()

	log.Printf("listening for HTTP requests on unix:%q", addr)
	if err := s.Serve(ln); err != nil {
		log.Fatalf("error in fasthttp server: %s", err)
	}
}

func serveHTTPTP() {
	ln := newTCPListener()
	inCompressType := compressType(*inCompress, "inCompress")
	s := httpteleport.Server{
		Handler:           httptpRequestHandler,
		Concurrency:       *concurrency,
		MaxBatchDelay:     *inDelay,
		ReduceMemoryUsage: true,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      5 * time.Second,
		CompressType:      inCompressType,
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
		Handler:           httpRequestHandler,
		Concurrency:       *concurrency,
		ReduceMemoryUsage: true,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
}

var (
	inRequestStart        = expvar.NewInt("inRequestStart")
	inRequestSuccess      = expvar.NewInt("inRequestSuccess")
	inRequestNon200       = expvar.NewInt("inRequestNon200")
	inRequestTimeoutError = expvar.NewInt("inRequestTimeoutError")
	inRequestOtherError   = expvar.NewInt("inRequestOtherError")
)

func httpRequestHandler(ctx *fasthttp.RequestCtx) {
	inRequestStart.Add(1)
	if *xForwardedFor {
		var buf [16]byte
		ip := fasthttp.AppendIPv4(buf[:0], ctx.RemoteIP())
		ctx.Request.Header.SetBytesV("X-Forwarded-For", ip)
	}

	c := leastLoadedClient()
	err := c.DoTimeout(&ctx.Request, &ctx.Response, *timeout)
	if err == nil {
		inRequestSuccess.Add(1)
		if ctx.Response.StatusCode() != fasthttp.StatusOK {
			inRequestNon200.Add(1)
		}
		return
	}

	ctx.ResetBody()
	fmt.Fprintf(ctx, "HTTP proxying error: %s", err)
	if err == fasthttp.ErrTimeout {
		inRequestTimeoutError.Add(1)
		ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
	} else {
		inRequestOtherError.Add(1)
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
	}
}

func httptpRequestHandler(ctx *fasthttp.RequestCtx) {
	inRequestStart.Add(1)
	// Reset 'Connection: close' request header in order to prevent
	// from closing keep-alive connections to -out servers.
	ctx.Request.Header.ResetConnectionClose()

	c := leastLoadedClient()
	err := c.DoTimeout(&ctx.Request, &ctx.Response, *timeout)
	if err == nil {
		inRequestSuccess.Add(1)
		if ctx.Response.StatusCode() != fasthttp.StatusOK {
			inRequestNon200.Add(1)
		}
		return
	}

	ctx.ResetBody()
	fmt.Fprintf(ctx, "httptp proxying error: %s", err)
	if err == httpteleport.ErrTimeout {
		inRequestTimeoutError.Add(1)
		ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
	} else {
		inRequestOtherError.Add(1)
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
	}
}

type client interface {
	DoTimeout(req *fasthttp.Request, resp *fasthttp.Response, timeout time.Duration) error
	PendingRequests() int
}

var upstreamClients []client

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
