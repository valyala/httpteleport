package httpteleport

import (
	"bytes"
	"fmt"
	"github.com/valyala/fasthttp"
	"net"
	"strings"
	"testing"
	"time"
)

func TestClientBodyStream(t *testing.T) {
	c := &Client{
		Dial: func(addr string) (net.Conn, error) {
			return nil, fmt.Errorf("no server")
		},
	}

	var req fasthttp.Request
	var resp fasthttp.Response
	req.SetRequestURI("http://foobar/baz")
	req.SetBodyStream(bytes.NewBufferString("foobarbaz"), -1)
	err := c.DoTimeout(&req, &resp, time.Second)
	if err == nil {
		t.Fatalf("expecting error")
	}
	if err != errNoBodyStream {
		t.Fatalf("unexpected error: %s. Expecting %s", err, errNoBodyStream)
	}
}

func TestClientNoServer(t *testing.T) {
	c := &Client{
		Dial: func(addr string) (net.Conn, error) {
			return nil, fmt.Errorf("no server")
		},
	}

	const iterations = 100
	resultCh := make(chan error, iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			var req fasthttp.Request
			var resp fasthttp.Response
			req.SetRequestURI("http://foobar/baz")
			resultCh <- c.DoTimeout(&req, &resp, 50*time.Millisecond)
		}()
	}

	for i := 0; i < iterations; i++ {
		var err error
		select {
		case err = <-resultCh:
		case <-time.After(time.Second):
			t.Fatalf("timeout")
		}
		if err == nil {
			t.Fatalf("expecting error on iteration %d", i)
		}
		switch {
		case err == ErrTimeout:
		case strings.Contains(err.Error(), "no server"):
		default:
			t.Fatalf("unexpected error on iteration %d: %s", i, err)
		}
	}
}

func TestClientTimeout(t *testing.T) {
	dialCh := make(chan struct{})
	c := &Client{
		Dial: func(addr string) (net.Conn, error) {
			<-dialCh
			return nil, fmt.Errorf("no dial")
		},
	}

	const iterations = 100
	resultCh := make(chan error, iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			var req fasthttp.Request
			var resp fasthttp.Response
			req.SetRequestURI("http://foobar/baz")
			resultCh <- c.DoTimeout(&req, &resp, 50*time.Millisecond)
		}()
	}

	for i := 0; i < iterations; i++ {
		var err error
		select {
		case err = <-resultCh:
		case <-time.After(time.Second):
			t.Fatalf("timeout")
		}
		if err == nil {
			t.Fatalf("expecting error on iteration %d", i)
		}
		switch {
		case err == ErrTimeout:
		default:
			t.Fatalf("unexpected error on iteration %d: %s", i, err)
		}
	}

	close(dialCh)
}
