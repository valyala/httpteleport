package httpteleport

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/valyala/fasthttp"
	"io"
	"net"
	"sync"
	"sync/atomic"
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

	lastErrLock sync.Mutex
	lastErr     error

	pendingRequests chan *clientWorkItem

	pendingResponses     map[uint32]*clientWorkItem
	pendingResponsesLock sync.Mutex

	reqID                uint32
	pendingRequestsCount uint32
}

var (
	// ErrTimeout is returned from timed out calls.
	ErrTimeout = fasthttp.ErrTimeout

	// ErrPendingRequestsOverflow is returned when Client cannot send
	// more requests to the server due to Client.MaxPendingRequests limit.
	ErrPendingRequestsOverflow = errors.New("Pending requests overflow. Increase Client.MaxPendingRequests, " +
		"reduce requests rate or speed up the server")
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

	n := int(atomic.AddUint32(&c.pendingRequestsCount, 1))

	if n >= c.maxPendingRequests() {
		atomic.AddUint32(&c.pendingRequestsCount, ^uint32(0))
		return c.getError(ErrPendingRequestsOverflow)
	}

	resp.Reset()

	wi := acquireClientWorkItem()
	wi.req = req
	wi.resp = resp
	wi.deadline = deadline
	if err := c.enqueueWorkItem(wi); err != nil {
		atomic.AddUint32(&c.pendingRequestsCount, ^uint32(0))
		releaseClientWorkItem(wi)
		return c.getError(err)
	}

	// the client guarantees that wi.done is unblocked before deadline,
	// so do not use select with time.After here.
	//
	// This saves memory and CPU resources.
	err := <-wi.done

	releaseClientWorkItem(wi)

	atomic.AddUint32(&c.pendingRequestsCount, ^uint32(0))

	return err
}

func (c *Client) enqueueWorkItem(wi *clientWorkItem) error {
	select {
	case c.pendingRequests <- wi:
		return nil
	default:
		// slow path
		select {
		case wiOld := <-c.pendingRequests:
			wiOld.done <- c.getError(ErrPendingRequestsOverflow)
			select {
			case c.pendingRequests <- wi:
				return nil
			default:
				return ErrPendingRequestsOverflow
			}
		default:
			return ErrPendingRequestsOverflow
		}
	}
}

func (c *Client) maxPendingRequests() int {
	maxPendingRequests := c.MaxPendingRequests
	if maxPendingRequests <= 0 {
		maxPendingRequests = DefaultMaxPendingRequests
	}
	return maxPendingRequests
}

func (c *Client) init() {
	n := c.maxPendingRequests()
	c.pendingRequests = make(chan *clientWorkItem, n)
	c.pendingResponses = make(map[uint32]*clientWorkItem, n)

	go func() {
		sleepDuration := 10 * time.Millisecond
		for {
			time.Sleep(sleepDuration)
			ok1 := c.unblockStaleRequests()
			ok2 := c.unblockStaleResponses()
			if ok1 || ok2 {
				sleepDuration = time.Duration(0.7 * float64(sleepDuration))
				if sleepDuration < 10*time.Millisecond {
					sleepDuration = 10 * time.Millisecond
				}
			} else {
				sleepDuration = time.Duration(1.5 * float64(sleepDuration))
				if sleepDuration > 10*time.Second {
					sleepDuration = 10 * time.Second
				}
			}
		}
	}()

	go c.worker()
}

func (c *Client) unblockStaleRequests() bool {
	found := false
	n := len(c.pendingRequests)
	t := time.Now()
	for i := 0; i < n; i++ {
		select {
		case wi := <-c.pendingRequests:
			if t.After(wi.deadline) {
				wi.done <- c.getError(ErrTimeout)
				found = true
			} else {
				if err := c.enqueueWorkItem(wi); err != nil {
					wi.done <- c.getError(err)
				}
			}
		default:
			return found
		}
	}
	return found
}

func (c *Client) unblockStaleResponses() bool {
	found := false
	t := time.Now()
	c.pendingResponsesLock.Lock()
	for reqID, wi := range c.pendingResponses {
		if t.After(wi.deadline) {
			wi.done <- c.getError(ErrTimeout)
			delete(c.pendingResponses, reqID)
			found = true
		}
	}
	c.pendingResponsesLock.Unlock()
	return found
}

// PendingRequests returns the number of pending requests at the moment.
//
// This function may be used either for informational purposes
// or for load balancing purposes.
func (c *Client) PendingRequests() int {
	return int(atomic.LoadUint32(&c.pendingRequestsCount))
}

func (c *Client) worker() {
	dial := c.Dial
	if dial == nil {
		dial = fasthttp.Dial
	}
	for {
		// Wait for the first request before dialing the server.
		wi := <-c.pendingRequests
		if err := c.enqueueWorkItem(wi); err != nil {
			wi.done <- c.getError(err)
		}

		conn, err := dial(c.Addr)
		if err != nil {
			c.setLastError(fmt.Errorf("cannot connect to %q: %s", c.Addr, err))
			time.Sleep(time.Second)
			continue
		}
		c.setLastError(err)
		laddr := conn.LocalAddr().String()
		raddr := conn.RemoteAddr().String()
		if err = c.serveConn(conn); err != nil {
			err = fmt.Errorf("error on connection %q<->%q: %s", laddr, raddr, err)
		}
		c.setLastError(err)
	}
}

func (c *Client) serveConn(conn net.Conn) error {
	br, bw, err := newBufioConn(conn, c.ReadBufferSize, c.WriteBufferSize, c.CompressType, c.TLSConfig, false)
	if err != nil {
		conn.Close()
		time.Sleep(time.Second)
		return err
	}

	readerDone := make(chan error, 1)
	go func() {
		readerDone <- c.connReader(br, conn)
	}()

	writerDone := make(chan error, 1)
	stopWriterCh := make(chan struct{})
	go func() {
		writerDone <- c.connWriter(bw, conn, stopWriterCh)
	}()

	select {
	case err = <-readerDone:
		close(stopWriterCh)
		conn.Close()
		<-writerDone
	case err = <-writerDone:
		conn.Close()
		<-readerDone
	}

	return err
}

func (c *Client) connWriter(bw *bufio.Writer, conn net.Conn, stopCh <-chan struct{}) error {
	var (
		wi  *clientWorkItem
		buf [4]byte
	)

	var (
		flushTimer    = getFlushTimer()
		flushCh       <-chan time.Time
		flushAlwaysCh = make(chan time.Time)
	)
	defer putFlushTimer(flushTimer)

	close(flushAlwaysCh)
	maxBatchDelay := c.MaxBatchDelay
	if maxBatchDelay < 0 {
		maxBatchDelay = 0
	}

	writeTimeout := c.WriteTimeout
	var lastWriteDeadline time.Time
	for {
		select {
		case wi = <-c.pendingRequests:
		default:
			// slow path
			select {
			case wi = <-c.pendingRequests:
			case <-stopCh:
				return nil
			case <-flushCh:
				if err := bw.Flush(); err != nil {
					return fmt.Errorf("cannot flush requests data to the server: %s", err)
				}
				flushCh = nil
				continue
			}
		}

		t := time.Now()
		if t.After(wi.deadline) {
			wi.done <- c.getError(ErrTimeout)
			continue
		}

		reqID := c.reqID
		c.reqID++

		if writeTimeout > 0 {
			// Optimization: update write deadline only if more than 25%
			// of the last write deadline exceeded.
			// See https://github.com/golang/go/issues/15133 for details.
			t := time.Now()
			if t.Sub(lastWriteDeadline) > (writeTimeout >> 2) {
				if err := conn.SetReadDeadline(t.Add(writeTimeout)); err != nil {
					return fmt.Errorf("cannot update write deadline: %s", err)
				}
				lastWriteDeadline = t
			}
		}

		b := appendUint32(buf[:0], reqID)
		if _, err := bw.Write(b); err != nil {
			err = fmt.Errorf("cannot send request ID to the server: %s", err)
			wi.done <- c.getError(err)
			return err
		}
		if err := wi.req.Write(bw); err != nil {
			err = fmt.Errorf("cannot send request to the server: %s", err)
			wi.done <- c.getError(err)
			return err
		}

		c.pendingResponsesLock.Lock()
		if _, ok := c.pendingResponses[reqID]; ok {
			c.pendingResponsesLock.Unlock()
			err := fmt.Errorf("request ID overflow. id=%d", reqID)
			wi.done <- c.getError(err)
			return err
		}
		c.pendingResponses[reqID] = wi
		c.pendingResponsesLock.Unlock()

		// re-arm flush channel
		if len(c.pendingRequests) == 0 {
			if maxBatchDelay > 0 {
				resetFlushTimer(flushTimer, maxBatchDelay)
				flushCh = flushTimer.C
			} else {
				flushCh = flushAlwaysCh
			}
		}
	}
}

func (c *Client) connReader(br *bufio.Reader, conn net.Conn) error {
	var (
		buf      [4]byte
		resp     *fasthttp.Response
		zeroResp fasthttp.Response
	)

	readTimeout := c.ReadTimeout
	var lastReadDeadline time.Time
	for {
		if readTimeout > 0 {
			// Optimization: update read deadline only if more than 25%
			// of the last read deadline exceeded.
			// See https://github.com/golang/go/issues/15133 for details.
			t := time.Now()
			if t.Sub(lastReadDeadline) > (readTimeout >> 2) {
				if err := conn.SetReadDeadline(t.Add(readTimeout)); err != nil {
					return fmt.Errorf("cannot update read deadline: %s", err)
				}
				lastReadDeadline = t
			}
		}

		if n, err := io.ReadFull(br, buf[:]); err != nil {
			if n == 0 {
				// Ignore error if no bytes are read, since
				// the server may just close the connection.
				return nil
			}
			return fmt.Errorf("cannot read response ID: %s", err)
		}

		reqID := bytes2Uint32(buf)

		c.pendingResponsesLock.Lock()
		wi := c.pendingResponses[reqID]
		delete(c.pendingResponses, reqID)
		c.pendingResponsesLock.Unlock()

		if wi == nil {
			// just skip response by reading it into zeroResp,
			// since wi may be already deleted
			// by unblockStaleResponses.
			resp = &zeroResp
		} else {
			resp = wi.resp
		}

		if err := resp.Read(br); err != nil {
			err = fmt.Errorf("cannot read response with ID %d: %s", reqID, err)
			if wi != nil {
				wi.done <- c.getError(err)
			}
			return err
		}

		if wi != nil {
			wi.done <- nil
		}
	}
}

func (c *Client) getError(err error) error {
	c.lastErrLock.Lock()
	lastErr := c.lastErr
	c.lastErrLock.Unlock()
	if lastErr != nil {
		return lastErr
	}
	return err
}

func (c *Client) setLastError(err error) {
	c.lastErrLock.Lock()
	c.lastErr = err
	c.lastErrLock.Unlock()
}

type clientWorkItem struct {
	req      *fasthttp.Request
	resp     *fasthttp.Response
	deadline time.Time
	done     chan error
}

func acquireClientWorkItem() *clientWorkItem {
	v := clientWorkItemPool.Get()
	if v == nil {
		v = &clientWorkItem{
			done: make(chan error, 1),
		}
	}
	wi := v.(*clientWorkItem)
	if len(wi.done) != 0 {
		panic("BUG: clientWorkItem.done must be empty")
	}
	return wi
}

func releaseClientWorkItem(wi *clientWorkItem) {
	if len(wi.done) != 0 {
		panic("BUG: clientWorkItem.done must be empty")
	}
	wi.req = nil
	wi.resp = nil
	clientWorkItemPool.Put(wi)
}

var clientWorkItemPool sync.Pool

func appendUint32(b []byte, n uint32) []byte {
	return append(b, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
}

func bytes2Uint32(b [4]byte) uint32 {
	return (uint32(b[3]) << 24) | (uint32(b[2]) << 16) | (uint32(b[1]) << 8) | uint32(b[0])
}
