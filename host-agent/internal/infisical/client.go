package infisical

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotConfigured = errors.New("Infisical client credentials are not configured")
	ErrInvalidScope  = errors.New("project, environment, and absolute secret path are required")
	ErrAuthRejected  = errors.New("Infisical rejected the client credentials")
)

// APIError deliberately excludes the response body, which may contain secrets.
type APIError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return fmt.Sprintf("Infisical API returned HTTP %d", e.StatusCode) }

type SecretName struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Client holds only a short-lived access token in memory. The bootstrap pair
// comes from the host process environment and is never returned to callers.
type Client struct {
	baseURL      *url.URL
	httpClient   *http.Client
	clientID     string
	clientSecret string
	now          func() time.Time

	mu          sync.Mutex
	token       string
	refreshAt   time.Time
	expiresAt   time.Time
	retryAt     time.Time
	authRevoked bool
}

func NewClient(rawURL, clientID, clientSecret string, httpClient *http.Client) (*Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, errors.New("Infisical URL must be an absolute origin without credentials, path, query, or fragment")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if parsed.Scheme != "http" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, errors.New("Infisical URL must use HTTPS except on loopback")
		}
	}
	if (clientID == "") != (clientSecret == "") {
		return nil, errors.New("INFISICAL_CLIENT_ID and INFISICAL_CLIENT_SECRET must be set together")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	clientCopy := *httpClient
	if clientCopy.Timeout == 0 {
		clientCopy.Timeout = 10 * time.Second
	}
	// Never forward the bootstrap pair or a bearer token to a redirect target.
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: parsed, httpClient: &clientCopy, clientID: clientID, clientSecret: clientSecret, now: time.Now}, nil
}

func (c *Client) Configured() bool { return c.clientID != "" && c.clientSecret != "" }

func ValidateScope(scope Scope) error {
	if strings.TrimSpace(scope.ProjectID) == "" || strings.TrimSpace(scope.Environment) == "" || !strings.HasPrefix(scope.SecretPath, "/") || strings.Contains(scope.SecretPath, "..") {
		return ErrInvalidScope
	}
	return nil
}

// ListSecretNames exposes names and paths only. Infisical's response may carry
// values even when viewSecretValue=false; the response type intentionally drops
// every other field before the caller or MCP layer sees it.
func (c *Client) ListSecretNames(ctx context.Context, projectID, environment, secretPath string) ([]SecretName, error) {
	if err := ValidateScope(Scope{ProjectID: projectID, Environment: environment, SecretPath: secretPath}); err != nil {
		return nil, err
	}
	query := url.Values{
		"projectId":       {projectID},
		"environment":     {environment},
		"secretPath":      {secretPath},
		"viewSecretValue": {"false"},
		"recursive":       {"false"},
		"include_imports": {"false"},
	}
	body, err := c.get(ctx, "/api/v4/secrets", query)
	if err != nil {
		return nil, err
	}
	var response struct {
		Secrets *[]struct {
			Key  string `json:"secretKey"`
			Path string `json:"secretPath"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Secrets == nil {
		return nil, ErrInvalidResponse
	}
	if len(*response.Secrets) > 1000 {
		return nil, ErrInvalidResponse
	}
	names := make([]SecretName, 0, len(*response.Secrets))
	for _, item := range *response.Secrets {
		if item.Key == "" {
			return nil, ErrInvalidResponse
		}
		names = append(names, SecretName{Name: item.Key, Path: item.Path})
	}
	return names, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, query, nil)
}

// request retries only GET after a 401. A write may have reached Infisical
// before an authorization failure, so replaying it could duplicate effects.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, payload any) ([]byte, error) {
	attempts := 1
	if method == http.MethodGet {
		attempts = 2
	}
	for attempt := 0; attempt < attempts; attempt++ {
		token, err := c.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		endpoint := c.baseURL.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})
		var bodyReader io.Reader
		if payload != nil {
			body, err := json.Marshal(payload)
			if err != nil || len(body) > 64<<10 {
				return nil, errors.New("invalid Infisical request body")
			}
			bodyReader = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bodyReader)
		if err != nil {
			return nil, errors.New("build Infisical request")
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			return nil, errors.New("Infisical request failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
		response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized {
			c.invalidateToken(token)
			if method == http.MethodGet && attempt == 0 {
				continue
			}
		}
		if response.StatusCode == http.StatusUnauthorized {
			c.mu.Lock()
			if c.token == token {
				c.token = ""
				c.refreshAt = time.Time{}
				c.expiresAt = time.Time{}
				c.retryAt = c.now().Add(30 * time.Second)
			}
			c.mu.Unlock()
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, &APIError{StatusCode: response.StatusCode, RetryAfter: retryAfter(response.Header.Get("Retry-After"))}
		}
		if readErr != nil || len(body) > 1<<20 {
			return nil, ErrInvalidResponse
		}
		return body, nil
	}
	return nil, &APIError{StatusCode: http.StatusUnauthorized}
}

func (c *Client) invalidateToken(observed string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == observed {
		c.token = ""
		c.refreshAt = time.Time{}
		c.expiresAt = time.Time{}
	}
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.token != "" && now.Before(c.refreshAt) {
		return c.token, nil
	}
	if c.authRevoked {
		return "", ErrAuthRejected
	}
	if now.Before(c.retryAt) {
		if c.token != "" && now.Before(c.expiresAt) {
			return c.token, nil
		}
		return "", errors.New("Infisical login is temporarily unavailable")
	}
	body, _ := json.Marshal(struct {
		ID     string `json:"clientId"`
		Secret string `json:"clientSecret"`
	}{c.clientID, c.clientSecret})
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "/api/v1/auth/universal-auth/login"})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return "", errors.New("build Infisical login request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		c.retryAt = now.Add(5 * time.Second)
		if c.token != "" && now.Before(c.expiresAt) {
			return c.token, nil
		}
		return "", errors.New("Infisical login failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		c.authRevoked = true
		return "", ErrAuthRejected
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		c.retryAt = now.Add(5 * time.Second)
		if c.token != "" && now.Before(c.expiresAt) {
			return c.token, nil
		}
		return "", &APIError{StatusCode: response.StatusCode}
	}
	var login struct {
		Token     string  `json:"accessToken"`
		ExpiresIn float64 `json:"expiresIn"`
		TokenType string  `json:"tokenType"`
	}
	maxTTLSeconds := float64((time.Duration(1<<63-1) / time.Second))
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&login); err != nil || login.Token == "" || !(login.ExpiresIn > 0 && login.ExpiresIn <= maxTTLSeconds) || login.TokenType != "Bearer" {
		return "", ErrInvalidResponse
	}
	c.token = login.Token
	validFor := time.Duration(login.ExpiresIn * float64(time.Second))
	c.refreshAt = now.Add(validFor * 8 / 10)
	c.expiresAt = now.Add(validFor)
	c.retryAt = time.Time{}
	return c.token, nil
}

// EnrollmentRequest is a host-internal metadata adapter, never a bridge route.
// It reuses the bounded machine-auth lifecycle; the caller supplies typed routes.
func (c *Client) EnrollmentRequest(ctx context.Context, method, path string, query url.Values, payload, out any) error {
	body, err := c.request(ctx, method, path, query, payload)
	if err != nil {
		return err
	}
	if out != nil && json.Unmarshal(body, out) != nil {
		return errors.New("invalid enrollment metadata")
	}
	return nil
}

func retryAfter(raw string) time.Duration {
	var d time.Duration
	if n, e := strconv.Atoi(raw); e == nil && n > 0 {
		if n > 3600 {
			return time.Hour
		}
		d = time.Duration(n) * time.Second
	} else if at, e := http.ParseTime(raw); e == nil {
		d = time.Until(at)
	}
	if d < 0 {
		return 0
	}
	if d > time.Hour {
		return time.Hour
	}
	return d
}
