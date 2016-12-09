package httpteleport

import (
	"bufio"
	"crypto/tls"
	"errors"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fastrpc"
	"net"
	"sync"
	"time"
)

// Client teleports http requests to the given httpteleport Server over a single
// connection.
//
// Use multiple clients for establishing multiple connections to the server
// if a single connection processing consumes 100% of a single CPU core
// on either multi-core client or server.
type Client struct {
	// Addr is the httpteleport Server address to connect to.
	Addr string

	// CompressType is the compression type used for requests.
	//
	// CompressFlate is used by default.
	CompressType CompressType

	// Dial is a custom function used for connecting to the Server.
	//
	// fasthttp.Dial is used by default.
	Dial func(addr string) (net.Conn, error)

	// TLSConfig is TLS (aka SSL) config used for establishing encrypted
	// connection to the server.
	//
	// Encrypted connections may be used for transferring sensitive
	// information over untrusted networks.
	//
	// By default connection to the server isn't encrypted.
	TLSConfig *tls.Config

	// MaxPendingRequests is the maximum number of pending requests
	// the client may issue until the server responds to them.
	//
	// DefaultMaxPendingRequests is used by default.
	MaxPendingRequests int

	// MaxBatchDelay is the maximum duration before pending requests
	// are sent to the server.
	//
	// Requests' batching may reduce network bandwidth usage and CPU usage.
	//
	// By default requests are sent immediately to the server.
	MaxBatchDelay time.Duration

	// Maximum duration for full response reading (including body).
	//
	// This also limits idle connection lifetime duration.
	//
	// By default response read timeout is unlimited.
	ReadTimeout time.Duration

	// Maximum duration for full request writing (including body).
	//
	// By default request write timeout is unlimited.
	WriteTimeout time.Duration

	// ReadBufferSize is the size for read buffer.
	//
	// DefaultReadBufferSize is used by default.
	ReadBufferSize int

	// WriteBufferSize is the size for write buffer.
	//
	// DefaultWriteBufferSize is used by default.
	WriteBufferSize int

	once sync.Once
	c    fastrpc.Client
}

var (
	// ErrTimeout is returned from timed out calls.
	ErrTimeout = fastrpc.ErrTimeout

	// ErrPendingRequestsOverflow is returned when Client cannot send
	// more requests to the server due to Client.MaxPendingRequests limit.
	ErrPendingRequestsOverflow = fastrpc.ErrPendingRequestsOverflow
)

// DoTimeout teleports the given request to the server set in Client.Addr.
//
// ErrTimeout is returned if the server didn't return response during
// the given timeout.
func (c *Client) DoTimeout(req *fasthttp.Request, resp *fasthttp.Response, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	return c.DoDeadline(req, resp, deadline)
}

var errNoBodyStream = errors.New("requests with body streams aren't supported")

// DoDeadline teleports the given request to the server set in Client.Addr.
//
// ErrTimeout is returned if the server didn't return response until
// the given deadline.
func (c *Client) DoDeadline(req *fasthttp.Request, resp *fasthttp.Response, deadline time.Time) error {
	c.once.Do(c.init)
	if req.IsBodyStream() {
		return errNoBodyStream
	}
	resp.Reset()
	return c.c.DoDeadline(requestWriter{req}, responseReader{resp}, deadline)
}

func (c *Client) init() {
	c.c.SniffHeader = sniffHeader
	c.c.ProtocolVersion = protocolVersion
	c.c.NewResponse = newResponse

	c.c.Addr = c.Addr
	c.c.CompressType = fastrpc.CompressType(c.CompressType)
	c.c.Dial = c.Dial
	c.c.TLSConfig = c.TLSConfig
	c.c.MaxPendingRequests = c.MaxPendingRequests
	c.c.MaxBatchDelay = c.MaxBatchDelay
	c.c.ReadTimeout = c.ReadTimeout
	c.c.WriteTimeout = c.WriteTimeout
	c.c.ReadBufferSize = c.ReadBufferSize
	c.c.WriteBufferSize = c.WriteBufferSize
}

// PendingRequests returns the number of pending requests at the moment.
//
// This function may be used either for informational purposes
// or for load balancing purposes.
func (c *Client) PendingRequests() int {
	return c.c.PendingRequests()
}

type requestWriter struct {
	*fasthttp.Request
}

func (w requestWriter) WriteRequest(bw *bufio.Writer) error {
	return w.Write(bw)
}

type responseReader struct {
	*fasthttp.Response
}

func (r responseReader) ReadResponse(br *bufio.Reader) error {
	return r.Read(br)
}

func newResponse() fastrpc.ResponseReader {
	return responseReader{&fasthttp.Response{}}
}
