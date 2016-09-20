package httpteleport

import (
	"bytes"
	"fmt"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
	"math/rand"
	"net"
	"testing"
	"time"
)

func TestServerTimeoutSerial(t *testing.T) {
	stopCh := make(chan struct{})
	h := func(ctx *fasthttp.RequestCtx) {
		<-stopCh
	}
	serverStop, c := newTestServerClient(h)

	if err := testTimeout(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	close(stopCh)

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerTimeoutConcurrent(t *testing.T) {
	stopCh := make(chan struct{})
	h := func(ctx *fasthttp.RequestCtx) {
		<-stopCh
	}
	serverStop, c := newTestServerClient(h)

	if err := testServerClientConcurrent(func() error { return testTimeout(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	close(stopCh)

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerBatchDelayRequestSerial(t *testing.T) {
	serverStop, c := newTestServerClient(testGetHandler)
	c.MaxBatchDelay = 10 * time.Millisecond

	if err := testGetBatchDelay(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerBatchDelayRequestConcurrent(t *testing.T) {
	serverStop, c := newTestServerClient(testGetHandler)
	c.MaxBatchDelay = 10 * time.Millisecond

	if err := testServerClientConcurrent(func() error { return testGetBatchDelay(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerBatchDelayResponseSerial(t *testing.T) {
	s := &Server{
		Handler:       testGetHandler,
		MaxBatchDelay: 10 * time.Millisecond,
	}
	serverStop, c := newTestServerClientExt(s)

	if err := testGetBatchDelay(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerBatchDelayResponseConcurrent(t *testing.T) {
	s := &Server{
		Handler:       testGetHandler,
		MaxBatchDelay: 10 * time.Millisecond,
	}
	serverStop, c := newTestServerClientExt(s)

	if err := testServerClientConcurrent(func() error { return testGetBatchDelay(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerBatchDelayRequestResponseSerial(t *testing.T) {
	s := &Server{
		Handler:       testGetHandler,
		MaxBatchDelay: 10 * time.Millisecond,
	}
	serverStop, c := newTestServerClientExt(s)
	c.MaxBatchDelay = 10 * time.Millisecond

	if err := testGetBatchDelay(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerBatchDelayRequestResponseConcurrent(t *testing.T) {
	s := &Server{
		Handler:       testGetHandler,
		MaxBatchDelay: 10 * time.Millisecond,
	}
	serverStop, c := newTestServerClientExt(s)
	c.MaxBatchDelay = 10 * time.Millisecond

	if err := testServerClientConcurrent(func() error { return testGetBatchDelay(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerConcurrencyLimit(t *testing.T) {
	const concurrency = 10
	doneCh := make(chan struct{})
	concurrencyCh := make(chan struct{}, concurrency)
	s := &Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			concurrencyCh <- struct{}{}
			<-doneCh
			ctx.SetBodyString("done")
		},
		Concurrency: concurrency,
	}
	serverStop, c := newTestServerClientExt(s)

	// issue concurrency requests to the server.
	resultCh := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			var req fasthttp.Request
			var resp fasthttp.Response
			req.SetRequestURI("http://foobar.com/baz")
			if err := c.DoTimeout(&req, &resp, time.Hour); err != nil {
				resultCh <- err
				return
			}
			statusCode := resp.StatusCode()
			if statusCode != fasthttp.StatusOK {
				resultCh <- fmt.Errorf("unexpected status code: %d. Expecting %d", statusCode, fasthttp.StatusOK)
				return
			}
			body := resp.Body()
			if string(body) != "done" {
				resultCh <- fmt.Errorf("unexpected body: %q. Expecting %q", body, "done")
				return
			}
			resultCh <- nil
		}()
	}

	// make sure the server called request handler for the issued requests
	for i := 0; i < concurrency; i++ {
		select {
		case <-concurrencyCh:
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout on iteration %d", i)
		}
	}

	// now all the requests must fail with 'concurrency limit exceeded'
	// error.
	for i := 0; i < 100; i++ {
		var req fasthttp.Request
		var resp fasthttp.Response
		req.SetRequestURI("http://aaa.bbb/cc")
		if err := c.DoTimeout(&req, &resp, time.Second); err != nil {
			t.Fatalf("unexpected error on iteration %d: %s", i, err)
		}
		statusCode := resp.StatusCode()
		if statusCode != fasthttp.StatusTooManyRequests {
			t.Fatalf("unexpected status code on iteration %d: %d. Expecting %d", i, statusCode, fasthttp.StatusTooManyRequests)
		}
	}

	// unblock requests to the server.
	close(doneCh)
	for i := 0; i < concurrency; i++ {
		select {
		case err := <-resultCh:
			if err != nil {
				t.Fatalf("unexpected error on iteration %d: %s", i, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout on iteration %d", i)
		}
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerGetSerial(t *testing.T) {
	serverStop, c := newTestServerClient(testGetHandler)

	if err := testGet(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerGetConcurrent(t *testing.T) {
	serverStop, c := newTestServerClient(testGetHandler)

	if err := testServerClientConcurrent(func() error { return testGet(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerPostSerial(t *testing.T) {
	serverStop, c := newTestServerClient(testPostHandler)

	if err := testPost(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerPostConcurrent(t *testing.T) {
	serverStop, c := newTestServerClient(testPostHandler)

	if err := testServerClientConcurrent(func() error { return testPost(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerSleepSerial(t *testing.T) {
	serverStop, c := newTestServerClient(testSleepHandler)

	if err := testSleep(c); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerSleepConcurrent(t *testing.T) {
	serverStop, c := newTestServerClient(testSleepHandler)

	if err := testServerClientConcurrent(func() error { return testSleep(c) }); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerMultiClientsSerial(t *testing.T) {
	serverStop, ln := newTestServer(testSleepHandler)

	f := func() error {
		c := newTestClient(ln)
		return testSleep(c)
	}
	if err := testServerClientConcurrent(f); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func TestServerMultiClientsConcurrent(t *testing.T) {
	serverStop, ln := newTestServer(testSleepHandler)

	f := func() error {
		c := newTestClient(ln)
		return testServerClientConcurrent(func() error { return testSleep(c) })
	}
	if err := testServerClientConcurrent(f); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err := serverStop(); err != nil {
		t.Fatalf("cannot shutdown server: %s", err)
	}
}

func testServerClientConcurrent(testFunc func() error) error {
	const concurrency = 10
	resultCh := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			resultCh <- testFunc()
		}()
	}

	for i := 0; i < concurrency; i++ {
		select {
		case err := <-resultCh:
			if err != nil {
				return fmt.Errorf("unexpected error: %s", err)
			}
		case <-time.After(time.Second):
			return fmt.Errorf("timeout")
		}
	}
	return nil
}

func testGet(c *Client) error {
	return testGetExt(c, 100)
}

func testGetBatchDelay(c *Client) error {
	return testGetExt(c, 10)
}

func testGetExt(c *Client, iterations int) error {
	for i := 0; i < iterations; i++ {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()

		host := fmt.Sprintf("foobar%d.com", i)
		req.Header.SetHost(host)
		req.SetRequestURI("/aaa")
		err := c.DoTimeout(req, resp, time.Second)
		if err != nil {
			return fmt.Errorf("unexpected error on iteration %d: %s", i, err)
		}
		statusCode := resp.StatusCode()
		if statusCode != fasthttp.StatusOK {
			return fmt.Errorf("unexpected status code on iteration %d: %d. Expecting %d", i, statusCode, fasthttp.StatusOK)
		}
		body := resp.Body()
		if string(body) != host {
			return fmt.Errorf("unexpected body on iteration %d: %q. Expecting %q", i, body, host)
		}

		fasthttp.ReleaseResponse(resp)
		fasthttp.ReleaseRequest(req)
	}
	return nil
}

func testPost(c *Client) error {
	var (
		req  fasthttp.Request
		resp fasthttp.Response
	)
	for i := 0; i < 100; i++ {
		req.Header.SetMethod("POST")
		req.SetRequestURI("http://foobar.com/aaa")
		expectedBody := fmt.Sprintf("body number %d", i)
		req.SetBodyString(expectedBody)
		err := c.DoTimeout(&req, &resp, time.Second)
		if err != nil {
			return fmt.Errorf("unexpected error on iteration %d: %s", i, err)
		}
		statusCode := resp.StatusCode()
		if statusCode != fasthttp.StatusOK {
			return fmt.Errorf("unexpected status code on iteration %d: %d. Expecting %d", i, statusCode, fasthttp.StatusOK)
		}
		body := resp.Body()
		if string(body) != expectedBody {
			return fmt.Errorf("unexpected body on iteration %d: %q. Expecting %q", i, body, expectedBody)
		}
	}
	return nil
}

func testSleep(c *Client) error {
	var (
		req  fasthttp.Request
		resp fasthttp.Response
	)
	expectedBodyPrefix := []byte("slept for ")
	for i := 0; i < 10; i++ {
		req.SetRequestURI("http://foobar.com/aaa")
		err := c.DoTimeout(&req, &resp, time.Second)
		if err != nil {
			return fmt.Errorf("unexpected error on iteration %d: %s", i, err)
		}
		statusCode := resp.StatusCode()
		if statusCode != fasthttp.StatusOK {
			return fmt.Errorf("unexpected status code on iteration %d: %d. Expecting %d", i, statusCode, fasthttp.StatusOK)
		}
		body := resp.Body()
		if !bytes.HasPrefix(body, expectedBodyPrefix) {
			return fmt.Errorf("unexpected body prefix on iteration %d: %q. Expecting %q", i, body, expectedBodyPrefix)
		}
	}
	return nil
}

func testTimeout(c *Client) error {
	var (
		req  fasthttp.Request
		resp fasthttp.Response
	)
	for i := 0; i < 10; i++ {
		req.SetRequestURI("http://foobar.com/aaa")
		err := c.DoTimeout(&req, &resp, 10*time.Millisecond)
		if err == nil {
			return fmt.Errorf("expecting non-nil error on iteration %d", i)
		}
		if err != ErrTimeout {
			return fmt.Errorf("unexpected error: %s. Expecting %s", err, ErrTimeout)
		}
	}
	return nil
}

func newTestServerClient(handler fasthttp.RequestHandler) (func() error, *Client) {
	serverStop, ln := newTestServer(handler)
	c := newTestClient(ln)
	return serverStop, c
}

func newTestServerClientExt(s *Server) (func() error, *Client) {
	serverStop, ln := newTestServerExt(s)
	c := newTestClient(ln)
	return serverStop, c
}

func newTestServer(handler fasthttp.RequestHandler) (func() error, *fasthttputil.InmemoryListener) {
	s := &Server{
		Handler: handler,
	}
	return newTestServerExt(s)
}

func newTestServerExt(s *Server) (func() error, *fasthttputil.InmemoryListener) {
	ln := fasthttputil.NewInmemoryListener()
	serverResultCh := make(chan error, 1)
	go func() {
		serverResultCh <- s.Serve(ln)
	}()

	return func() error {
		ln.Close()
		select {
		case err := <-serverResultCh:
			if err != nil {
				return fmt.Errorf("unexpected error: %s", err)
			}
		case <-time.After(time.Second):
			return fmt.Errorf("timeout")
		}
		return nil
	}, ln
}

func newTestClient(ln *fasthttputil.InmemoryListener) *Client {
	return &Client{
		Dial: func(addr string) (net.Conn, error) {
			return ln.Dial()
		},
	}
}

func testGetHandler(ctx *fasthttp.RequestCtx) {
	host := ctx.Host()
	ctx.Write(host)
}

func testPostHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetBody(ctx.Request.Body())
}

func testSleepHandler(ctx *fasthttp.RequestCtx) {
	sleepDuration := time.Duration(rand.Intn(30)) * time.Millisecond
	time.Sleep(sleepDuration)
	fmt.Fprintf(ctx, "slept for %s", sleepDuration)
}
