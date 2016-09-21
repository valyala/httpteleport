package httpteleport

import (
	"bufio"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/tcplisten"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Server is a server accepting requests from httpteleport Client.
type Server struct {
	// Handler must process incoming http requests.
	Handler fasthttp.RequestHandler

	// Concurrency is the maximum number of concurrent goroutines
	// with Server.Handler the server may run.
	//
	// DefaultConcurrency is used by default.
	Concurrency int

	// MaxBatchDelay is the maximum duration before ready responses
	// are sent to the client.
	//
	// Responses' batching may reduce network bandwidth usage and CPU usage.
	//
	// By default responses are sent immediately to the client.
	// DefaultMaxBatchDelay is used by default.
	MaxBatchDelay time.Duration

	// ReadBufferSize is the size for read buffer.
	//
	// DefaultReadBufferSize is used by default.
	ReadBufferSize int

	// WriteBufferSize is the size for write buffer.
	//
	// DefaultWriteBufferSize is used by default.
	WriteBufferSize int

	// Logger, which is used by RequestCtx.Logger().
	//
	// Standard logger from log package is used by default.
	Logger Logger

	workItemPool  sync.Pool

	concurrencyCount uint32
}

func (s *Server) concurrency() int {
	concurrency := s.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	return concurrency
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
	for {
		conn, err := ln.Accept()
		if err != nil {
			if conn != nil {
				panic("BUG: net.Listener returned non-nil conn and non-nil error")
			}
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				s.logger().Printf("httpteleport.Server: temporary error when accepting new connections: %s", netErr)
				time.Sleep(time.Second)
				continue
			}
			if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
				s.logger().Printf("httpteleport.Server: permanent error when accepting new connections: %s", err)
				return err
			}
			return nil
		}
		if conn == nil {
			panic("BUG: net.Listener returned (nil, nil)")
		}

		go func() {
			err := s.serveConn(conn)
			s.logger().Printf("httpteleport.Server: error on connection %q<->%q: %s", conn.LocalAddr(), conn.RemoteAddr(), err)
		}()
	}
}

func (s *Server) serveConn(conn net.Conn) error {
	br, bw := newBufioConn(conn, s.ReadBufferSize, s.WriteBufferSize)
	stopCh := make(chan struct{})

	pendingResponses := make(chan *serverWorkItem, s.concurrency())
	readerDone := make(chan error, 1)
	go func() {
		readerDone <- s.connReader(br, conn, pendingResponses, stopCh)
	}()

	writerDone := make(chan error, 1)
	go func() {
		writerDone <- s.connWriter(bw, pendingResponses, stopCh)
	}()

	var err error
	select {
	case err = <-readerDone:
		conn.Close()
		close(stopCh)
		<-writerDone
	case err = <-writerDone:
		conn.Close()
		close(stopCh)
		<-readerDone
	}
	return err
}

func (s *Server) connReader(br *bufio.Reader, conn net.Conn, pendingResponses chan<- *serverWorkItem, stopCh <-chan struct{}) error {
	handler := s.Handler
	if handler == nil {
		panic("BUG: Server.Handler must be set")
	}
	logger := s.logger()
	concurrency := s.concurrency()
	for {
		wi := s.acquireWorkItem()
		if _, err := io.ReadFull(br, wi.reqID[:]); err != nil {
			return fmt.Errorf("cannot read request ID: %s", err)
		}

		wi.ctx.Init2(conn, logger)

		if err := wi.ctx.Request.Read(br); err != nil {
			return fmt.Errorf("cannot read request: %s", err)
		}

		n := int(atomic.AddUint32(&s.concurrencyCount, 1))
		if n > concurrency {
			atomic.AddUint32(&s.concurrencyCount, ^uint32(0))

			fmt.Fprintf(&wi.ctx, "concurrency limit exceeded: %d. Increase Server.Concurrency or decrease load on the server", concurrency)
			wi.ctx.SetStatusCode(fasthttp.StatusTooManyRequests)

			select {
			case pendingResponses <- wi:
			default:
				select {
				case pendingResponses <- wi:
				case <-stopCh:
					return nil
				}
			}
			continue
		}

		go func() {
			handler(&wi.ctx)
			if wi.ctx.IsBodyStream() {
				panic("chunked responses aren't supported")
			}
			select {
			case pendingResponses <- wi:
			default:
				select {
				case pendingResponses <- wi:
				case <-stopCh:
				}
			}

			atomic.AddUint32(&s.concurrencyCount, ^uint32(0))
		}()
	}
}

func (s *Server) connWriter(bw *bufio.Writer, pendingResponses <-chan *serverWorkItem, stopCh <-chan struct{}) error {
	var wi *serverWorkItem

	var (
		flushTimer    = time.NewTimer(time.Hour * 24 * 30)
		flushCh       <-chan time.Time
		flushAlwaysCh = make(chan time.Time)
	)
	close(flushAlwaysCh)
	maxBatchDelay := s.MaxBatchDelay
	if maxBatchDelay < 0 {
		maxBatchDelay = 0
	}

	for {
		select {
		case wi = <-pendingResponses:
		default:
			select {
			case wi = <-pendingResponses:
			case <-stopCh:
				return nil
			case <-flushCh:
				if err := bw.Flush(); err != nil {
					return fmt.Errorf("cannot flush response data to client: %s", err)
				}
				flushCh = nil
				continue
			}
		}

		if _, err := bw.Write(wi.reqID[:]); err != nil {
			return fmt.Errorf("cannot write response ID: %d", err)
		}
		if err := wi.ctx.Response.Write(bw); err != nil {
			return fmt.Errorf("cannot write response: %s", err)
		}

		s.releaseWorkItem(wi)

		// re-arm flush channel
		if len(pendingResponses) == 0 {
			if maxBatchDelay > 0 {
				if !flushTimer.Stop() {
					select {
					case <-flushTimer.C:
					default:
					}
				}
				flushTimer.Reset(maxBatchDelay)
				flushCh = flushTimer.C
			} else {
				flushCh = flushAlwaysCh
			}
		}
	}
}

type serverWorkItem struct {
	ctx   fasthttp.RequestCtx
	reqID [4]byte
}

func (s *Server) acquireWorkItem() *serverWorkItem {
	v := s.workItemPool.Get()
	if v == nil {
		return &serverWorkItem{}
	}
	return v.(*serverWorkItem)
}

func (s *Server) releaseWorkItem(wi *serverWorkItem) {
	s.workItemPool.Put(wi)
}

// Logger is used for logging formatted messages.
type Logger interface {
	// Printf must have the same semantics as log.Printf.
	Printf(format string, args ...interface{})
}

var defaultLogger = Logger(log.New(os.Stderr, "", log.LstdFlags))

func (s *Server) logger() Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return defaultLogger
}
