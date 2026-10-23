package hostbridge

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client relays the bounded Doctor command with a dedicated bridge bearer.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

func New(rawURL, token, caFile string) (*Client, error) {
	base, err := url.Parse(rawURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("host bridge URL must be an HTTPS origin")
	}
	if len(token) < 32 {
		return nil, errors.New("host bridge bearer is unavailable")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		certificate, err := os.ReadFile(caFile)
		if err != nil || len(certificate) > 64<<10 {
			return nil, errors.New("host bridge CA file unavailable")
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(certificate) {
			return nil, errors.New("host bridge CA file contains no certificate")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	return &Client{
		base:  base,
		token: token,
		http:  &http.Client{Timeout: 35 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := ""
	switch r.URL.Path {
	case "/v1/host/doctor":
		path = "/v1/doctor"
	default:
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10+1))
	if err != nil || len(body) > 64<<10 {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	endpoint := c.base.ResolveReference(&url.URL{Path: path})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "host_bridge_unavailable", http.StatusBadGateway)
		return
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		http.Error(w, "host_bridge_unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(content) > 1<<20 || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "invalid_host_bridge_response", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(content)
}
