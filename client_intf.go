package enproxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	X_HTTPCONN_ID        = "X-HTTPConn-Id"
	X_HTTPCONN_DEST_ADDR = "X-HTTPConn-Dest-Addr"
	X_HTTPCONN_EOF       = "X-HTTPConn-EOF"
)

var (
	defaultPollInterval    = 50 * time.Millisecond
	defaultIdleInterval    = 5 * time.Millisecond
	firstWriteIdleInterval = 1000 * time.Hour // just needs to be a really large value
	defaultIdleTimeout     = 10 * time.Second
)

// Client is a net.Conn that tunnels its data via an httpconn.Proxy using HTTP
// requests and responses.  It assumes that streaming requests are not supported
// by the underlying servers/proxies, and so uses a polling technique similar to
// the one used by meek, but different in that data is not encoded as JSON.
// https://trac.torproject.org/projects/tor/wiki/doc/AChildsGardenOfPluggableTransports#Undertheencryption.
//
// The basics flow is as follows:
//   1. Accept writes, piping these to the proxy as the body of an http request
//   2. Continue to pipe the writes until the pause between consecutive writes
//      exceeds the IdleInterval, at which point we finish the request body
//   3. Accept reads, reading the data from the response body until EOF is
//      is reached or the gap between consecutive reads exceeds the
//      IdleInterval. If EOF wasn't reached, whenever we next accept reads, we
//      will continue to read from the same response until EOF is reached, then
//      move on to the next response.
//   4. Go back to accepting writes (step 1)
//   5. If no writes are received for more than PollInterval, issue an empty
//      request in order to pick up any new data received on the proxy, start
//      accepting reads (step 3)
//
type Client struct {
	Config *Config

	writeRequests    chan []byte      // requests to write
	writeResponses   chan rwResponse  // responses for writes
	readRequests     chan []byte      // requests to read
	readResponses    chan rwResponse  // responses for reads
	lastActivityTime time.Time        // time of last read or write
	stop             chan interface{} // stop notification
	closedMutex      sync.RWMutex     // mutex controlling access to closed flag
	closed           bool             // whether or not this Client is closed

	// Addr: the host:port of the destination server that we're trying to reach
	Addr string

	id              string         // unique identifier for this connection
	netAddr         net.Addr       // the resolved net.Addr
	proxyConn       net.Conn       // a underlying connection to the proxy
	bufReader       *bufio.Reader  // buffered reader for proxyConn
	req             *http.Request  // the current request being used to send data
	pipeReader      *io.PipeReader // pipe reader for current request body
	pipeWriter      *io.PipeWriter // pipe writer to current request body
	resp            *http.Response // the current response being used to read data
	lastRequestTime time.Time      // time of last request
}

type dialFunc func(addr string) (net.Conn, error)

type newRequestFunc func(method string, body io.Reader) (*http.Request, error)

// rwResponse is a response to a read or write
type rwResponse struct {
	n   int
	err error
}

type Config struct {
	// DialProxy: function to open a connection to the proxy
	DialProxy dialFunc

	// NewRequest: function to create a new request to the proxy
	NewRequest newRequestFunc

	// IdleTimeout: how long to wait for a read before switching to writing
	IdleTimeout time.Duration

	// PollInterval: how frequently to poll (i.e. create a new request/response)
	// , defaults to 50 ms
	PollInterval time.Duration

	// IdleInterval: how long to wait for the next write/read before switching
	// to read/write (defaults to 1 millisecond)
	IdleInterval time.Duration
}

func (c *Client) LocalAddr() net.Addr {
	if c.proxyConn == nil {
		return nil
	} else {
		return c.proxyConn.LocalAddr()
	}
}

func (c *Client) RemoteAddr() net.Addr {
	return c.netAddr
}

func (c *Client) Write(b []byte) (n int, err error) {
	if c.isClosed() {
		return 0, io.EOF
	}
	c.writeRequests <- b
	res, ok := <-c.writeResponses
	if !ok {
		return 0, io.EOF
	} else {
		return res.n, res.err
	}
}

func (c *Client) Read(b []byte) (n int, err error) {
	if c.isClosed() {
		return 0, io.EOF
	}
	c.readRequests <- b
	res, ok := <-c.readResponses
	if !ok {
		return 0, io.EOF
	} else {
		return res.n, res.err
	}
}

func (c *Client) Close() error {
	if c.markClosed() {
		c.stop <- true
	}
	return nil
}

func (c *Client) SetDeadline(t time.Time) error {
	panic("SetDeadline not implemented")
}

func (c *Client) SetReadDeadline(t time.Time) error {
	panic("SetReadDeadline not implemented")
}

func (c *Client) SetWriteDeadline(t time.Time) error {
	panic("SetWriteDeadline not implemented")
}
