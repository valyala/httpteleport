package main

import (
	"expvar"
	"flag"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/expvarhandler"
	"log"
	"net"
	"sync/atomic"
)

var (
	expvarAddr = flag.String("expvarAddr", "localhost:8040", "TCP address for exporting httptp metrics. They are exported "+
		"at http://expvarAddr/expvar page")
)

func initExpvarServer() {
	if *expvarAddr == "" {
		return
	}

	log.Printf("exporting stats at http://%s/expvar", *expvarAddr)

	go func() {
		if err := fasthttp.ListenAndServe(*expvarAddr, expvarHandler); err != nil {
			log.Fatalf("error in expvar server: %s", err)
		}
	}()
}

func expvarHandler(ctx *fasthttp.RequestCtx) {
	path := ctx.Path()
	if string(path) == "/expvar" {
		expvarhandler.ExpvarHandler(ctx)
	} else {
		ctx.Error("unsupported path", fasthttp.StatusBadRequest)
	}
}

func newExpvarDial(dial fasthttp.DialFunc) fasthttp.DialFunc {
	return func(addr string) (net.Conn, error) {
		conn, err := dial(addr)
		if err != nil {
			return nil, err
		}
		outConns.Add(1)
		return &expvarConn{Conn: conn}, nil
	}
}

type expvarConn struct {
	net.Conn
	closed uint32
}

func (c *expvarConn) Close() error {
	if atomic.AddUint32(&c.closed, 1) == 0 {
		outConns.Add(-1)
	}
	return c.Conn.Close()
}

var outConns = expvar.NewInt("outConns")
