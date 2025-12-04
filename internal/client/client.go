package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	LocalAddr   string
	RelayAddr   string
	Token       string
	Domain      string
	InspectPort int

	httpClient *http.Client
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

// Connect establishes HTTP/2 tunnel to relay server
func (c *Client) Connect(ctx context.Context) error {
	// TODO: Implement HTTP/2 tunnel connection
	return fmt.Errorf("not implemented")
}

// Run starts the tunnel and processes incoming requests
func (c *Client) Run(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// TODO: Read requests from tunnel, forward to local, send responses back
	<-ctx.Done()
	return ctx.Err()
}

// ReadResponse reads the full response body
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
