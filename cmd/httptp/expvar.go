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
			outDialError.Add(1)
			return nil, err
		}
		outConns.Add(1)
		outDialSuccess.Add(1)
		return &expvarConn{
			Conn:      conn,
			conns:     outConns,
			bytesSent: outBytesSent,
			bytesRecv: outBytesRecv,
		}, nil
	}
}

type expvarConn struct {
	net.Conn

	conns     *expvar.Int
	bytesSent *expvar.Int
	bytesRecv *expvar.Int

	closed uint32
}

func (c *expvarConn) Close() error {
	if atomic.AddUint32(&c.closed, 1) == 1 {
		c.conns.Add(-1)
	}
	return c.Conn.Close()
}

func (c *expvarConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.bytesSent.Add(int64(n))
	return n, err
}

func (c *expvarConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.bytesRecv.Add(int64(n))
	return n, err
}

var (
	outDialSuccess = expvar.NewInt("outDialSuccess")
	outDialError   = expvar.NewInt("outDialError")
	outConns       = expvar.NewInt("outConns")
	outBytesSent   = expvar.NewInt("outBytesSent")
	outBytesRecv   = expvar.NewInt("outBytesRecv")
)

type expvarListener struct {
	net.Listener
}

func (ln *expvarListener) Accept() (net.Conn, error) {
	conn, err := ln.Listener.Accept()
	if err != nil {
		inAcceptError.Add(1)
		return nil, err
	}
	inAcceptSuccess.Add(1)
	inConns.Add(1)
	return &expvarConn{
		Conn:      conn,
		conns:     inConns,
		bytesSent: inBytesSent,
		bytesRecv: inBytesRecv,
	}, nil
}

var (
	inAcceptSuccess = expvar.NewInt("inAcceptSuccess")
	inAcceptError   = expvar.NewInt("inAcceptError")
	inConns         = expvar.NewInt("inConns")
	inBytesSent     = expvar.NewInt("inBytesSent")
	inBytesRecv     = expvar.NewInt("inBytesRecv")
)
