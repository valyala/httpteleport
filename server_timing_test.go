package httpteleport

import (
	"crypto/tls"
	"github.com/valyala/fasthttp"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkEndToEndGetNoDelay1(b *testing.B) {
	benchmarkEndToEndGet(b, 1, 0, CompressNone, false, false)
}

func BenchmarkEndToEndGetNoDelay10(b *testing.B) {
	benchmarkEndToEndGet(b, 10, 0, CompressNone, false, false)
}

func BenchmarkEndToEndGetNoDelay100(b *testing.B) {
	benchmarkEndToEndGet(b, 100, 0, CompressNone, false, false)
}

func BenchmarkEndToEndGetNoDelay1000(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 0, CompressNone, false, false)
}

func BenchmarkEndToEndGetNoDelay10K(b *testing.B) {
	benchmarkEndToEndGet(b, 10000, 0, CompressNone, false, false)
}

func BenchmarkEndToEndGetDelay1ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressNone, false, false)
}

func BenchmarkEndToEndGetDelay2ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 2*time.Millisecond, CompressNone, false, false)
}

func BenchmarkEndToEndGetDelay4ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 4*time.Millisecond, CompressNone, false, false)
}

func BenchmarkEndToEndGetDelay8ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 8*time.Millisecond, CompressNone, false, false)
}

func BenchmarkEndToEndGetDelay16ms(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 16*time.Millisecond, CompressNone, false, false)
}

func BenchmarkEndToEndGetCompressNone(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressNone, false, false)
}

func BenchmarkEndToEndGetCompressFlate(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressFlate, false, false)
}

func BenchmarkEndToEndGetCompressSnappy(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressSnappy, false, false)
}

func BenchmarkEndToEndGetTLSCompressNone(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressNone, true, false)
}

func BenchmarkEndToEndGetTLSCompressFlate(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressFlate, true, false)
}

func BenchmarkEndToEndGetTLSCompressSnappy(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, time.Millisecond, CompressSnappy, true, false)
}

func BenchmarkEndToEndGetPipeline1(b *testing.B) {
	benchmarkEndToEndGet(b, 1, 0, CompressNone, false, true)
}

func BenchmarkEndToEndGetPipeline10(b *testing.B) {
	benchmarkEndToEndGet(b, 10, 0, CompressNone, false, true)
}

func BenchmarkEndToEndGetPipeline100(b *testing.B) {
	benchmarkEndToEndGet(b, 100, 0, CompressNone, false, true)
}

func BenchmarkEndToEndGetPipeline1000(b *testing.B) {
	benchmarkEndToEndGet(b, 1000, 0, CompressNone, false, true)
}

func benchmarkEndToEndGet(b *testing.B, parallelism int, batchDelay time.Duration, compressType CompressType, isTLS, pipelineRequests bool) {
	var tlsConfig *tls.Config
	if isTLS {
		tlsConfig = newTestServerTLSConfig()
	}
	var serverBatchDelay time.Duration
	if batchDelay > 0 {
		serverBatchDelay = 100 * time.Microsecond
	}
	expectedBody := "Hello world foobar baz aaa bbb ccc ddd eee gklj kljsdfsdf" +
		"sdfasdaf asdf asdf dsa fasd fdasf afsgfdsg ertytrshdsf fds gf" +
		"dfagsf asglsdkflaskdflkqowqiot asdkljlp 0293 4u09u0sd9fulksj lksfj lksdfj sdf" +
		"sfjkko9u iodjsf-[9j lksdjf;lkasdj02r fsd fhjas;klfj asd;lfjwjfsd; "
	s := &Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetBodyString(expectedBody)
		},
		Concurrency:      parallelism * runtime.NumCPU(),
		MaxBatchDelay:    serverBatchDelay,
		CompressType:     compressType,
		TLSConfig:        tlsConfig,
		PipelineRequests: pipelineRequests,
	}
	serverStop, ln := newTestServerExt(s)

	var cc []*Client
	for i := 0; i < runtime.NumCPU(); i++ {
		c := newTestClient(ln)
		c.MaxPendingRequests = s.Concurrency
		c.MaxBatchDelay = batchDelay
		c.CompressType = compressType
		if isTLS {
			c.TLSConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		cc = append(cc, c)
	}
	var clientIdx uint32

	deadline := time.Now().Add(time.Hour)
	b.SetParallelism(parallelism)
	b.SetBytes(int64(len(expectedBody)))
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
