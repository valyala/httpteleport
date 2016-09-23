package main

import (
	"flag"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/httpteleport"
	"github.com/valyala/tcplisten"
	"log"
	"strings"
	"time"
)

var (
	maxServerConns = flag.Int("maxServerConns", 1000, "The maximum number of open connections to each upstream "+
		"HTTP server listed in serverAddr")

	batchDelay  = flag.Duration("batchDelay", time.Millisecond, "How long to wait before flushing responses back to httptc")
	concurrency = flag.Int("concurrency", 100000, "The maximum number of concurrent requests the server may handle")
	listenAddr  = flag.String("listenAddr", ":8043", "TCP address to listen to for httptc connections")
	serverAddr  = flag.String("serverAddr", "127.0.0.1:8044", "Comma-separated list of upstream HTTP server TCP addresses "+
		"to forward requests to.\n"+
		"Each request is forwared to the least loaded upstream server")
	timeout = flag.Duration("timeout", time.Second, "Maximum duration for waiting response from upstream HTTP servers")
)

func main() {
	flag.Parse()

	for _, addr := range strings.Split(*serverAddr, ",") {
		c := &fasthttp.HostClient{
			Addr:     addr,
			MaxConns: *maxServerConns,
		}
		cs = append(cs, c)
	}

	cfg := tcplisten.Config{}
	ln, err := cfg.NewListener("tcp4", *listenAddr)
	if err != nil {
		log.Fatalf("cannot listen to -listenAddr=%q: %s", *listenAddr, err)
	}

	s := httpteleport.Server{
		Handler:       requestHandler,
		Concurrency:   *concurrency,
		MaxBatchDelay: *batchDelay,
	}

	log.Printf("listening for httptc connections on %q", *listenAddr)
	log.Printf("forwarding requests to upstream HTTP servers %q", *serverAddr)

	if err := s.Serve(ln); err != nil {
		log.Fatalf("error in fasthttp server: %s", err)
	}
}

var cs []*fasthttp.HostClient

func requestHandler(ctx *fasthttp.RequestCtx) {
	c := leastLoadedClient()
	err := c.DoTimeout(&ctx.Request, &ctx.Response, *timeout)
	if err == nil {
		return
	}

	ctx.ResetBody()
	fmt.Fprintf(ctx, "httpts proxying error: %s", err)
	if err == fasthttp.ErrTimeout {
		ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
	} else {
		ctx.SetStatusCode(fasthttp.StatusBadGateway)
	}
}

func leastLoadedClient() *fasthttp.HostClient {
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
