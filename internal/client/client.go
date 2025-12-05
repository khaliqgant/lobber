package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lobber-dev/lobber/internal/tunnel"
)

type Client struct {
	LocalAddr   string
	RelayAddr   string
	Token       string
	Domain      string
	InspectPort int

	httpClient *http.Client
	conn       net.Conn
	bufrw      *bufio.ReadWriter
	onReady    func() // Called when client is ready to receive requests
}

func New(localAddr, relayAddr, token, domain string) *Client {
	return &Client{
		LocalAddr: localAddr,
		RelayAddr: relayAddr,
		Token:     token,
		Domain:    domain,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetOnReady sets a callback that's invoked when the client is ready to receive requests
func (c *Client) SetOnReady(fn func()) {
	c.onReady = fn
}

// ForwardToLocal forwards an incoming request to the local server
func (c *Client) ForwardToLocal(req *http.Request) (*http.Response, error) {
	// Lazy-init httpClient if not set
	if c.httpClient == nil {
		c.httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	// Build the local URL
	localURL, err := url.Parse(c.LocalAddr)
	if err != nil {
		return nil, fmt.Errorf("parse local addr: %w", err)
	}

	// Create a new request to the local server
	localURL.Path = req.URL.Path
	localURL.RawQuery = req.URL.RawQuery

	localReq, err := http.NewRequestWithContext(req.Context(), req.Method, localURL.String(), req.Body)
	if err != nil {
		return nil, fmt.Errorf("create local request: %w", err)
	}

	// Copy headers
	for k, v := range req.Header {
		localReq.Header[k] = v
	}

	// Send to local server
	resp, err := c.httpClient.Do(localReq)
	if err != nil {
		return nil, fmt.Errorf("local request: %w", err)
	}

	return resp, nil
}

// Connect establishes tunnel connection to relay server
func (c *Client) Connect(ctx context.Context) error {
	// Parse relay URL
	relayURL, err := url.Parse(c.RelayAddr)
	if err != nil {
		return fmt.Errorf("parse relay addr: %w", err)
	}

	// Determine host:port
	host := relayURL.Host
	if !strings.Contains(host, ":") {
		if relayURL.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	// Connect to relay
	conn, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	c.conn = conn

	// Send HTTP request to /_lobber/connect
	c.bufrw = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))

	// Write HTTP request
	fmt.Fprintf(c.bufrw, "POST /_lobber/connect HTTP/1.1\r\n")
	fmt.Fprintf(c.bufrw, "Host: %s\r\n", relayURL.Host)
	fmt.Fprintf(c.bufrw, "Authorization: Bearer %s\r\n", c.Token)
	fmt.Fprintf(c.bufrw, "X-Lobber-Domain: %s\r\n", c.Domain)
	fmt.Fprintf(c.bufrw, "Connection: Upgrade\r\n")
	fmt.Fprintf(c.bufrw, "\r\n")
	if err := c.bufrw.Flush(); err != nil {
		conn.Close()
		return fmt.Errorf("write request: %w", err)
	}

	// Read HTTP response
	resp, err := http.ReadResponse(c.bufrw.Reader, nil)
	if err != nil {
		conn.Close()
		return fmt.Errorf("read response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.Close()
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("connect failed: %s - %s", resp.Status, string(body))
	}

	return nil
}

// Run starts the tunnel and processes incoming requests
func (c *Client) Run(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Send ready frame to signal we're ready to receive requests
	if err := tunnel.EncodeReady(c.bufrw); err != nil {
		if c.conn != nil {
			c.conn.Close()
		}
		return fmt.Errorf("send ready frame: %w", err)
	}
	if err := c.bufrw.Flush(); err != nil {
		if c.conn != nil {
			c.conn.Close()
		}
		return fmt.Errorf("flush ready frame: %w", err)
	}

	// Signal ready via callback if set
	if c.onReady != nil {
		c.onReady()
	}

	// Process requests until context is cancelled
	errCh := make(chan error, 1)
	go func() {
		for {
			// Check context
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}

			// Read request from relay
			req, err := tunnel.DecodeRequest(c.bufrw)
			if err != nil {
				errCh <- fmt.Errorf("decode request: %w", err)
				return
			}

			// Forward to local server
			resp, err := c.forwardRequest(ctx, req)
			if err != nil {
				// Send error response
				resp = &tunnel.Response{
					ID:         req.ID,
					StatusCode: http.StatusBadGateway,
					Headers:    map[string][]string{"Content-Type": {"text/plain"}},
					Body:       []byte("local forward error: " + err.Error()),
				}
			}

			// Send response back through tunnel
			if err := tunnel.EncodeResponse(c.bufrw, resp); err != nil {
				errCh <- fmt.Errorf("encode response: %w", err)
				return
			}
			c.bufrw.Flush()
		}
	}()

	select {
	case <-ctx.Done():
		if c.conn != nil {
			c.conn.Close()
		}
		return ctx.Err()
	case err := <-errCh:
		if c.conn != nil {
			c.conn.Close()
		}
		return err
	}
}

// forwardRequest forwards a tunnel request to the local server
func (c *Client) forwardRequest(ctx context.Context, req *tunnel.Request) (*tunnel.Response, error) {
	// Build local URL
	localURL, err := url.Parse(c.LocalAddr)
	if err != nil {
		return nil, fmt.Errorf("parse local addr: %w", err)
	}
	localURL.Path = req.Path

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, localURL.String(), io.NopCloser(strings.NewReader(string(req.Body))))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Copy headers
	for k, v := range req.Headers {
		httpReq.Header[k] = v
	}

	// Lazy init httpClient
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	// Send to local server
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("local request: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return &tunnel.Response{
		ID:         req.ID,
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
	}, nil
}

// ReadResponse reads the full response body
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
