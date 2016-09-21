package httpteleport

import (
	"github.com/valyala/fasthttp"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkEndToEndGetNoDelay1(b *testing.B) {
	benchmarkEndToEndGet(b, 1, 0)
}

func BenchmarkEndToEndGetNoDelay10(b *testing.B) {
	benchmarkEndToEndGet(b, 10, 0)
}

func BenchmarkEndToEndGetNoDelay100(b *testing.B) {
	benchmarkEndToEndGet(b, 100, 0)
}

func BenchmarkEndToEndGetNoDelay1000(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 0)
}

func BenchmarkEndToEndGetNoDelay10K(b *testing.B) {
	benchmarkEndToEndGet(b, 10000, 0)
}

func BenchmarkEndToEndGetDelay1ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond)
}

func BenchmarkEndToEndGetDelay2ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 2*time.Millisecond)
}

func BenchmarkEndToEndGetDelay4ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 4*time.Millisecond)
}

func BenchmarkEndToEndGetDelay8ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 8*time.Millisecond)
}

func BenchmarkEndToEndGetDelay16ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 16*time.Millisecond)
}

func benchmarkEndToEndGet(b *testing.B, parallelism int, batchDelay time.Duration) {
	expectedBody := "Hello world"
	s := &Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetBodyString(expectedBody)
		},
		Concurrency:   parallelism * runtime.NumCPU(),
		MaxBatchDelay: batchDelay,
	}
	serverStop, ln := newTestServerExt(s)

	var cc []*Client
	for i := 0; i < runtime.NumCPU(); i++ {
		c := newTestClient(ln)
		c.MaxPendingRequests = s.Concurrency
		c.MaxBatchDelay = batchDelay
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
