package gigachat

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DefaultBaseURL   = "https://api.giga.chat/v1"
	DefaultAuthURL   = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"
	DefaultScope     = "GIGACHAT_API_PERS"
	DefaultModel     = "GigaChat-2-Pro"
	maxResponseBytes = 8 << 20
)

var ErrNotConfigured = errors.New("GigaChat authorization key is not configured")

// APIError never includes provider response bodies or credentials.
type APIError struct {
	StatusCode int
}

func (e *APIError) Error() string { return fmt.Sprintf("GigaChat returned HTTP %d", e.StatusCode) }

type Options struct {
	AuthKey string
	Scope   string
	BaseURL string
	AuthURL string
	CAFile  string
	Timeout time.Duration
}

type Client struct {
	authKey, scope, baseURL, authURL string
	httpClient                       *http.Client
	// The channel serializes refreshes while allowing waiters to cancel.
	tokenLock   chan struct{}
	accessToken string
	expiresAt   time.Time
}

func NewClient(opts Options) (*Client, error) {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.AuthURL == "" {
		opts.AuthURL = DefaultAuthURL
	}
	if opts.Scope == "" {
		opts.Scope = DefaultScope
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.Timeout < 0 {
		return nil, errors.New("GigaChat timeout must be positive")
	}
	for _, endpoint := range []string{opts.BaseURL, opts.AuthURL} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("GigaChat endpoints must be HTTPS URLs without credentials, query or fragment")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, errors.New("cannot read GigaChat CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA certificates: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("GigaChat CA file contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &Client{
		authKey: strings.TrimSpace(opts.AuthKey), scope: opts.Scope,
		baseURL: strings.TrimRight(opts.BaseURL, "/"), authURL: opts.AuthURL,
		httpClient: &http.Client{Timeout: opts.Timeout, Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		tokenLock: make(chan struct{}, 1),
	}, nil
}

func (c *Client) Chat(ctx context.Context, input ChatRequest) (ChatResponse, error) {
	var output ChatResponse
	if c.authKey == "" {
		return output, ErrNotConfigured
	}
	body, err := json.Marshal(input)
	if err != nil {
		return output, errors.New("cannot encode GigaChat request")
	}
	token, err := c.token(ctx, "")
	if err != nil {
		return output, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return output, fmt.Errorf("create GigaChat request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		err = c.do(req, &output)
		var apiErr *APIError
		if attempt != 0 || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
			return output, err
		}
		token, err = c.token(ctx, token)
		if err != nil {
			return output, err
		}
	}
	return output, errors.New("GigaChat authentication retry exhausted")
}

func (c *Client) token(ctx context.Context, rejected string) (string, error) {
	select {
	case c.tokenLock <- struct{}{}:
		defer func() { <-c.tokenLock }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.accessToken != "" && c.accessToken != rejected && time.Now().Add(30*time.Second).Before(c.expiresAt) {
		return c.accessToken, nil
	}
	// Do not reuse an expired/rejected token if refresh fails.
	c.accessToken = ""
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate GigaChat request ID: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authURL, strings.NewReader(url.Values{"scope": {c.scope}}.Encode()))
	if err != nil {
		return "", fmt.Errorf("create GigaChat auth request: %w", err)
	}
	req.Header.Set("Authorization", "Basic "+c.authKey)
	req.Header.Set("RqUID", fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	if err := c.do(req, &result); err != nil {
		return "", fmt.Errorf("authorize GigaChat: %w", err)
	}
	// OAuth uses Unix milliseconds; accept seconds used in older documentation too.
	expires := time.UnixMilli(result.ExpiresAt)
	if result.ExpiresAt < 100_000_000_000 {
		expires = time.Unix(result.ExpiresAt, 0)
	}
	if strings.TrimSpace(result.AccessToken) == "" || !expires.After(time.Now()) {
		return "", errors.New("GigaChat returned an empty or expired access token")
	}
	c.accessToken, c.expiresAt = result.AccessToken, expires
	return c.accessToken, nil
}

func (c *Client) do(req *http.Request, target any) error {
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request GigaChat: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return &APIError{StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read GigaChat response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return errors.New("GigaChat response exceeds size limit")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("invalid GigaChat JSON response")
	}
	return nil
}
