package httpteleport

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fastrpc"
	"github.com/valyala/tcplisten"
	"net"
	"time"
)

// Server accepts requests from httpteleport Client.
type Server struct {
	// Handler must process incoming http requests.
	//
	// Handler mustn't use the following features:
	//
	//   - Connection hijacking, i.e. RequestCtx.Hijack
	//   - Streamed response bodies, i.e. RequestCtx.*BodyStream*
	Handler fasthttp.RequestHandler

	// CompressType is the compression type used for responses.
	//
	// CompressFlate is used by default.
	CompressType CompressType

	// Concurrency is the maximum number of concurrent goroutines
	// with Server.Handler the server may run.
	//
	// DefaultConcurrency is used by default.
	Concurrency int

	// TLSConfig is TLS (aka SSL) config used for accepting encrypted
	// client connections.
	//
	// Encrypted connections may be used for transferring sensitive
	// information over untrusted networks.
	//
	// By default server accepts only unencrypted connections.
	TLSConfig *tls.Config

	// MaxBatchDelay is the maximum duration before ready responses
	// are sent to the client.
	//
	// Responses' batching may reduce network bandwidth usage and CPU usage.
	//
	// By default responses are sent immediately to the client.
	MaxBatchDelay time.Duration

	// Maximum duration for reading the full request (including body).
	//
	// This also limits the maximum lifetime for idle connections.
	//
	// By default request read timeout is unlimited.
	ReadTimeout time.Duration

	// Maximum duration for writing the full response (including body).
	//
	// By default response write timeout is unlimited.
	WriteTimeout time.Duration

	// ReduceMemoryUsage leads to reduced memory usage at the cost
	// of higher CPU usage if set to true.
	//
	// Memory usage reduction is disabled by default.
	ReduceMemoryUsage bool

	// ReadBufferSize is the size for read buffer.
	//
	// DefaultReadBufferSize is used by default.
	ReadBufferSize int

	// WriteBufferSize is the size for write buffer.
	//
	// DefaultWriteBufferSize is used by default.
	WriteBufferSize int

	// Logger used for logging.
	//
	// Standard logger from log package is used by default.
	Logger fasthttp.Logger

	// PipelineRequests enables requests' pipelining.
	//
	// Requests from a single client are processed serially
	// if is set to true.
	//
	// Enabling requests' pipelining may be useful in the following cases:
	//
	//   - if requests from a single client must be processed serially;
	//   - if the Server.Handler doesn't block and maximum throughput
	//     must be achieved for requests' processing.
	//
	// By default requests from a single client are processed concurrently.
	PipelineRequests bool

	s fastrpc.Server
}

// ListenAndServe serves httpteleport requests accepted from the given
// TCP address.
func (s *Server) ListenAndServe(addr string) error {
	var cfg tcplisten.Config
	ln, err := cfg.NewListener("tcp4", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve serves httpteleport requests accepted from the given listener.
func (s *Server) Serve(ln net.Listener) error {
	s.init()
	return s.s.Serve(ln)
}

func (s *Server) init() {
	if s.Handler == nil {
		panic("BUG: Server.Handler must be set")
	}

	s.s.SniffHeader = sniffHeader
	s.s.ProtocolVersion = protocolVersion
	s.s.NewHandlerCtx = s.newHandlerCtx
	s.s.Handler = s.requestHandler

	s.s.CompressType = fastrpc.CompressType(s.CompressType)
	s.s.Concurrency = s.Concurrency
	s.s.TLSConfig = s.TLSConfig
	s.s.MaxBatchDelay = s.MaxBatchDelay
	s.s.ReadTimeout = s.ReadTimeout
	s.s.WriteTimeout = s.WriteTimeout
	s.s.ReadBufferSize = s.ReadBufferSize
	s.s.WriteBufferSize = s.WriteBufferSize
	s.s.Logger = s.Logger
	s.s.PipelineRequests = s.PipelineRequests
}

type handlerCtx struct {
	ctx *fasthttp.RequestCtx
	s   *Server
}

func (s *Server) newHandlerCtx() fastrpc.HandlerCtx {
	return &handlerCtx{
		ctx: &fasthttp.RequestCtx{},
		s:   s,
	}
}

func (ctx *handlerCtx) Init(conn net.Conn, logger fasthttp.Logger) {
	ctx.ctx.Init2(conn, logger, ctx.s.ReduceMemoryUsage)
}

func (ctx *handlerCtx) ReadRequest(br *bufio.Reader) error {
	return ctx.ctx.Request.Read(br)
}

func (ctx *handlerCtx) WriteResponse(bw *bufio.Writer) error {
	err := ctx.ctx.Response.Write(bw)

	// Response is no longer needed, so reset it in order to release
	// resources occupied by the response.
	ctx.ctx.Response.Reset()

	return err
}

func (ctx *handlerCtx) ConcurrencyLimitError(concurrency int) {
	fmt.Fprintf(ctx.ctx, "concurrency limit exceeded: %d. Increase Server.Concurrency or decrease load on the server", concurrency)
	ctx.ctx.SetStatusCode(fasthttp.StatusTooManyRequests)
}

func (s *Server) requestHandler(ctxv fastrpc.HandlerCtx) fastrpc.HandlerCtx {
	ctx := ctxv.(*handlerCtx)
	s.Handler(ctx.ctx)
	if ctx.ctx.IsBodyStream() {
		panic("chunked responses aren't supported")
	}
	if ctx.ctx.Hijacked() {
		panic("hijacking isn't supported")
	}
	timeoutResp := ctx.ctx.LastTimeoutErrorResponse()
	if timeoutResp != nil {
		// The current ctx may be still in use by the handler.
		// So create new one for passing to pendingResponses.
		ctxNew := s.newHandlerCtx().(*handlerCtx)
		timeoutResp.CopyTo(&ctxNew.ctx.Response)
		ctx = ctxNew
	}

	// Request is no longer needed, so reset it in order
	// to free up resources occupied by the request.
	ctx.ctx.Request.Reset()

	return ctx
}
