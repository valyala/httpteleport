package main

import (
	"flag"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/expvarhandler"
	"log"
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
