package main

import (
	"flag"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/httpteleport"
	"github.com/valyala/tcplisten"
	"log"
	"time"
)

var (
	reusePort   = flag.Bool("reusePort", false, "Whether to enable SO_REUSEPORT on listenAddr")
	serverConns = flag.Int("serverConns", 1, "The number of concurrent connections to httpts. "+
		"Usually a single connection is enough. Increase serverConns if httptc consumes more than 100% of a single CPU core")

	batchDelay  = flag.Duration("batchDelay", time.Millisecond, "How long to wait before flushing incoming requests to httpts")
	concurrency = flag.Int("concurrency", 100000, "The maximum number of concurrent incoming connections the client may handle")
	listenAddr  = flag.String("listenAddr", ":8042", "TCP address to listen to for incoming HTTP requests")
	serverAddr  = flag.String("serverAddr", "127.0.0.1:8043", "TCP address of httpts server to route incoming requests to")
	timeout     = flag.Duration("timeout", time.Second, "Maximum duration for waiting response from httpteleport server")
)

func main() {
	flag.Parse()

	for i := 0; i < *serverConns; i++ {
		c := &httpteleport.Client{
			Addr:               *serverAddr,
			MaxBatchDelay:      *batchDelay,
			MaxPendingRequests: *concurrency,
		}
		cs = append(cs, c)
	}

	cfg := tcplisten.Config{
		ReusePort: *reusePort,
	}
	ln, err := cfg.NewListener("tcp4", *listenAddr)
	if err != nil {
		log.Fatalf("cannot listen to -listenAddr=%q: %s", *listenAddr, err)
	}

	s := fasthttp.Server{
		Handler:     requestHandler,
		Concurrency: *concurrency,
	}

	log.Printf("listening for HTTP requests on %q", *listenAddr)
	log.Printf("forwarding requests to httpts %q", *serverAddr)

	if err := s.Serve(ln); err != nil {
		log.Fatalf("error in fasthttp server: %s", err)
	}
}

var cs []*httpteleport.Client

func requestHandler(ctx *fasthttp.RequestCtx) {
	var buf [16]byte
	ip := fasthttp.AppendIPv4(buf[:0], ctx.RemoteIP())
	ctx.Request.Header.SetBytesV("X-Forwarded-For", ip)

	c := leastLoadedClient()
	err := c.DoTimeout(&ctx.Request, &ctx.Response, *timeout)
	if err == nil {
		return
	}

	ctx.ResetBody()
	fmt.Fprintf(ctx, "httptc proxying error: %s", err)
	if err == httpteleport.ErrTimeout {
		ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
	} else {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
	}
}

func leastLoadedClient() *httpteleport.Client {
	minC := cs[0]
	minN := minC.PendingRequests()
	if minN == 0 {
		return minC
	}
	for _, c := range cs[1:] {
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
