package httpteleport

import (
	"bufio"
	"bytes"
	"compress/flate"
	"fmt"
	"github.com/golang/snappy"
	"io"
	"net"
	"time"
)

const (
	// DefaultMaxPendingRequests is the default number of pending requests
	// a single Client may queue before sending them to the server.
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

const (
	protocolVersion1 = 0
)

// CompressType is a compression type used for connections.
type CompressType byte

const (
	// CompressNone disables connection compression.
	//
	// CompressNone may be used in the following cases:
	//
	//   * If network bandwidth between client and server is unlimited.
	//   * If client and server are located on the same physical host.
	//   * If other CompressType values consume a lot of CPU resources.
	//
	CompressNone = CompressType(1)

	// CompressFlate uses compress/flate with default
	// compression level for connection compression.
	//
	// CompressFlate may be used in the following cases:
	//
	//     * If network bandwidth between client and server is limited.
	//     * If client and server are located on distinct physical hosts.
	//     * If both client and server have enough CPU resources
	//       for compression.
	//
	CompressFlate = CompressType(0)

	// CompressSnappy uses snappy compression.
	//
	// CompressSnappy vs CompressFlate comparison:
	//
	//     * CompressSnappy consumes less CPU resources.
	//     * compressSnappy consumes more network bandwidth.
	//
	CompressSnappy = CompressType(2)
)

func newBufioConn(conn net.Conn, readBufferSize, writeBufferSize int,
	writeCompressType CompressType, isServer bool) (*bufio.Reader, *bufio.Writer, error) {

	handshake := handshakeClient
	if isServer {
		handshake = handshakeServer
	}
	readCompressType, err := handshake(conn, writeCompressType)
	if err != nil {
		return nil, nil, fmt.Errorf("error in handshake: %s", err)
	}

	r := io.Reader(conn)
	switch readCompressType {
	case CompressNone:
	case CompressFlate:
		r = flate.NewReader(r)
	case CompressSnappy:
		r = snappy.NewReader(r)
	default:
		return nil, nil, fmt.Errorf("unknown read CompressType: %v", readCompressType)
	}
	if readBufferSize <= 0 {
		readBufferSize = DefaultReadBufferSize
	}
	br := bufio.NewReaderSize(r, readBufferSize)

	w := io.Writer(conn)
	switch writeCompressType {
	case CompressNone:
	case CompressFlate:
		zw, err := flate.NewWriter(w, flate.DefaultCompression)
		if err != nil {
			panic(fmt.Sprintf("BUG: flate.NewWriter(%d) returned non-nil err: %s", flate.DefaultCompression, err))
		}
		w = &writeFlusher{w: zw}
	case CompressSnappy:
		// From the docs at https://godoc.org/github.com/golang/snappy#NewWriter :
		// There is no need to Flush or Close such a Writer,
		// so don't wrap it into writeFlusher.
		w = snappy.NewWriter(w)
	default:
		return nil, nil, fmt.Errorf("unknown write CompressType: %v", writeCompressType)
	}
	if writeBufferSize <= 0 {
		writeBufferSize = DefaultWriteBufferSize
	}
	bw := bufio.NewWriterSize(w, writeBufferSize)
	return br, bw, nil
}

func handshakeServer(conn net.Conn, compressType CompressType) (CompressType, error) {
	readCompressType, err := handshakeRead(conn)
	if err != nil {
		return 0, err
	}
	if err := handshakeWrite(conn, compressType); err != nil {
		return 0, err
	}
	return readCompressType, nil
}

func handshakeClient(conn net.Conn, compressType CompressType) (CompressType, error) {
	if err := handshakeWrite(conn, compressType); err != nil {
		return 0, err
	}
	return handshakeRead(conn)
}

func handshakeWrite(conn net.Conn, compressType CompressType) error {
	if err := conn.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("cannot set write timeout: %s", err)
	}

	if _, err := conn.Write(sniffHeader); err != nil {
		return fmt.Errorf("cannot write sniffHeader: %s", err)
	}

	var buf [2]byte
	buf[0] = protocolVersion1
	buf[1] = byte(compressType)
	if _, err := conn.Write(buf[:]); err != nil {
		return fmt.Errorf("cannot write connection header: %s", err)
	}
	if err := conn.SetWriteDeadline(zeroTime); err != nil {
		return fmt.Errorf("cannot reset write timeout: %s", err)
	}
	return nil
}

func handshakeRead(conn net.Conn) (CompressType, error) {
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return 0, fmt.Errorf("cannot set read timeout: %s", err)
	}

	sniffBuf := make([]byte, len(sniffHeader))
	if _, err := io.ReadFull(conn, sniffBuf); err != nil {
		return 0, fmt.Errorf("cannot read sniffHeader: %s", err)
	}
	if !bytes.Equal(sniffBuf, sniffHeader) {
		return 0, fmt.Errorf("invalid sniffHeader read: %q. Expecting %q", sniffBuf, sniffHeader)
	}

	var buf [2]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return 0, fmt.Errorf("cannot read connection header: %s", err)
	}
	if buf[0] != protocolVersion1 {
		return 0, fmt.Errorf("server returned unknown protocol version: %d", buf[0])
	}
	if err := conn.SetReadDeadline(zeroTime); err != nil {
		return 0, fmt.Errorf("cannot reset read timeout: %s", err)
	}
	return CompressType(buf[1]), nil
}

var sniffHeader = []byte("httpteleport")

var zeroTime time.Time

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
