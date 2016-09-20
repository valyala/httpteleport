package httpteleport

import (
	"github.com/valyala/fasthttp"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkEndToEndGet1(b *testing.B) {
	benchmarkEndToEndGet(b, 1)
}

func BenchmarkEndToEndGet10(b *testing.B) {
	benchmarkEndToEndGet(b, 10)
}

func BenchmarkEndToEndGet100(b *testing.B) {
	benchmarkEndToEndGet(b, 100)
}

func BenchmarkEndToEndGet1000(b *testing.B) {
	benchmarkEndToEndGet(b, 1000)
}

func BenchmarkEndToEndGet10K(b *testing.B) {
	benchmarkEndToEndGet(b, 10000)
}

func benchmarkEndToEndGet(b *testing.B, parallelism int) {
	expectedBody := "Hello world"
	s := &Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetBodyString(expectedBody)
		},
		Concurrency: parallelism * runtime.NumCPU() * 2,
	}
	serverStop, ln := newTestServerExt(s)

	var cc []*Client
	for i := 0; i < runtime.NumCPU(); i++ {
		c := newTestClient(ln)
		c.MaxPendingRequests = s.Concurrency
		cc = append(cc, c)
	}
	var clientIdx uint32

	deadline := time.Now().Add(time.Hour)
	b.SetParallelism(parallelism)
	b.RunParallel(func(pb *testing.PB) {
		n := atomic.AddUint32(&clientIdx, 1)
		c := cc[int(n)%len(cc)]
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()
		for pb.Next() {
			req.Header.SetHost("foobar")
			req.SetRequestURI("/foo/bar")
			if err := c.DoDeadline(req, resp, deadline); err != nil {
				b.Fatalf("unexpected error: %s", err)
			}
			statusCode := resp.StatusCode()
			if statusCode != fasthttp.StatusOK {
				b.Fatalf("unexpected status code: %d. Expecting %d", statusCode, fasthttp.StatusOK)
			}
			body := resp.Body()
			if string(body) != expectedBody {
				b.Fatalf("unexpected body: %q. Expecting %q", body, expectedBody)
			}
		}
		fasthttp.ReleaseResponse(resp)
		fasthttp.ReleaseRequest(req)
	})

	if err := serverStop(); err != nil {
		b.Fatalf("cannot shutdown server: %s", err)
	}
}
