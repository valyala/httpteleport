package httpteleport

import (
	"github.com/valyala/fastrpc"
)

const protocolVersion = 0

var sniffHeader = "httpteleport"

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
	CompressNone = CompressType(fastrpc.CompressNone)

	// CompressFlate uses compress/flate with default
	// compression level for connection compression.
	//
	// CompressFlate may be used in the following cases:
	//
	//     * If network bandwidth between client and server is limited.
	//     * If client and server are located on distinct physical hosts.
	//     * If both client and server have enough CPU resources
	//       for compression processing.
	//
	CompressFlate = CompressType(fastrpc.CompressFlate)

	// CompressSnappy uses snappy compression.
	//
	// CompressSnappy vs CompressFlate comparison:
	//
	//     * CompressSnappy consumes less CPU resources.
	//     * CompressSnappy consumes more network bandwidth.
	//
	CompressSnappy = CompressType(fastrpc.CompressSnappy)
)
