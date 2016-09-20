package httpteleport

import (
	"bufio"
	"compress/flate"
	"fmt"
	"io"
	"net"
)

const (
	// DefaultMaxPendingRequests is the default number of pending requests
	// a single Client queue before sending them to the server.
	//
	// This parameter may be overriden by Client.MaxPendingRequests.
	DefaultMaxPendingRequests = 1000

	// DefaultConcurrency is the default maximum number of concurrent
	// Server.Handler goroutines the server may run.
	DefaultConcurrency = 10000
)

const (
	// DefaultReadBufferSize is the default size for read buffers.
	DefaultReadBufferSize = 64 * 1024

	// DefaultWriteBufferSize is the default size for write buffers.
	DefaultWriteBufferSize = 64 * 1024
)

func newBufioConn(conn net.Conn, readBufferSize, writeBufferSize int) (*bufio.Reader, *bufio.Writer) {
	compress := conn.RemoteAddr().String() != conn.LocalAddr().String()
	r := io.Reader(conn)
	if compress {
		r = flate.NewReader(r)
	}
	if readBufferSize <= 0 {
		readBufferSize = DefaultReadBufferSize
	}
	br := bufio.NewReaderSize(r, readBufferSize)

	w := io.Writer(conn)
	if compress {
		zw, err := flate.NewWriter(w, flate.DefaultCompression)
		if err != nil {
			panic(fmt.Sprintf("BUG: flate.NewWriter(%d) returned non-nil err: %s", flate.DefaultCompression, err))
		}
		w = &writeFlusher{w: zw}
	}
	if writeBufferSize <= 0 {
		writeBufferSize = DefaultWriteBufferSize
	}
	bw := bufio.NewWriterSize(w, writeBufferSize)
	return br, bw
}

type writeFlusher struct {
	w *flate.Writer
}

func (wf *writeFlusher) Write(p []byte) (int, error) {
	n, err := wf.w.Write(p)
	if err != nil {
		return n, err
	}
	if err := wf.w.Flush(); err != nil {
		return 0, err
	}
	return n, nil
}
