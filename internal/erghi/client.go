// Package erghi is a small client for the Erghi REST API authenticated with a
// workspace API key (client_id + client_secret, exchanged for a short-lived
// bearer token).
package erghi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxResponseBytes caps what a single API call can put into a model's context.
const maxResponseBytes = 1 << 20

// refreshMargin renews the token this long before it expires.
const refreshMargin = 5 * time.Minute

// Config holds what the client needs to reach Erghi.
type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	UserAgent    string
}

// Client calls the Erghi API as one workspace API key.
type Client struct {
	base      *url.URL
	id        string
	secret    string
	http      *http.Client
	userAgent string

	mu      sync.Mutex
	token   string
	expires time.Time
	claims  TokenClaims
}

// TokenClaims are the parts of an exchanged token the server reports back.
// They are read without verifying the signature: they only drive which tools
// are offered, and the API enforces every scope itself.
type TokenClaims struct {
	ClientID    string   `json:"sub"`
	WorkspaceID string   `json:"workspace_id"`
	Scopes      []string `json:"-"`
	ExpiresAt   int64    `json:"exp"`
}

// APIError is a non-2xx answer from the API.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" {
		return fmt.Sprintf("erghi API %d (%s): %s", e.Status, e.Code, msg)
	}
	return fmt.Sprintf("erghi API %d: %s", e.Status, msg)
}

// New validates the configuration. It refuses plain-HTTP API URLs except for
// loopback hosts, so a key is never sent in clear text over a network.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("ERGHI_CLIENT_ID and ERGHI_CLIENT_SECRET are required")
	}
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid ERGHI_API_URL %q", cfg.BaseURL)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("ERGHI_API_URL must use https (plain http is only allowed for localhost), got %q", cfg.BaseURL)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = "erghi-mcp"
	}
	return &Client{base: u, id: cfg.ClientID, secret: cfg.ClientSecret, http: hc, userAgent: ua}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// BaseURL is the API root, without a trailing slash.
func (c *Client) BaseURL() string { return c.base.String() }

// Claims exchanges a token if needed and returns what it carries.
func (c *Client) Claims(ctx context.Context) (TokenClaims, error) {
	if _, err := c.bearer(ctx, false); err != nil {
		return TokenClaims{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claims, nil
}

// Token returns a valid bearer token, exchanging the key when needed.
func (c *Client) Token(ctx context.Context) (string, error) { return c.bearer(ctx, false) }

func (c *Client) bearer(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.token != "" && time.Until(c.expires) > refreshMargin {
		return c.token, nil
	}

	body, _ := json.Marshal(map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     c.id,
		"client_secret": c.secret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.String()+"/api/v1/auth/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		apiErr := parseError(resp.StatusCode, raw)
		if resp.StatusCode == http.StatusUnauthorized {
			apiErr.Message = "the API key was rejected (wrong client id/secret, revoked, or expired)"
		}
		return "", apiErr
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil || tok.AccessToken == "" {
		return "", errors.New("token exchange: malformed response")
	}
	claims, err := decodeClaims(tok.AccessToken)
	if err != nil {
		return "", err
	}
	c.token = tok.AccessToken
	c.expires = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	c.claims = claims
	return c.token, nil
}

func decodeClaims(token string) (TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return TokenClaims{}, errors.New("token exchange: token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return TokenClaims{}, fmt.Errorf("token exchange: %w", err)
	}
	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return TokenClaims{}, fmt.Errorf("token exchange: %w", err)
	}
	// "permission" is a string when there is one scope and an array otherwise.
	var perms struct {
		Permission json.RawMessage `json:"permission"`
	}
	_ = json.Unmarshal(payload, &perms)
	if len(perms.Permission) > 0 {
		var many []string
		if json.Unmarshal(perms.Permission, &many) == nil {
			claims.Scopes = many
		} else {
			var one string
			if json.Unmarshal(perms.Permission, &one) == nil {
				claims.Scopes = []string{one}
			}
		}
	}
	return claims, nil
}

// Do sends a JSON request and returns the raw JSON response body. A nil body
// sends no payload. It retries once with a fresh token on 401, in case the
// cached token was invalidated early.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	var payload []byte
	contentType := ""
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
		contentType = "application/json"
	}
	return c.send(ctx, method, path, query, contentType, payload)
}

// File is one file part of a multipart request.
type File struct {
	Field, Name string
	Content     []byte
}

// DoMultipart sends form fields and one file as multipart/form-data, for the
// upload endpoints, and returns the raw JSON response body.
func (c *Client) DoMultipart(ctx context.Context, method, path string, fields map[string]string, file File) (json.RawMessage, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	part, err := w.CreateFormFile(file.Field, file.Name)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(file.Content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return c.send(ctx, method, path, nil, w.FormDataContentType(), buf.Bytes())
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, contentType string, payload []byte) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		token, err := c.bearer(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		target := c.base.String() + path
		if len(query) > 0 {
			target += "?" + query.Encode()
		}
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", method, path, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			apiErr := parseError(resp.StatusCode, raw)
			if apiErr.Code != "api_key_inactive" {
				continue
			}
			return nil, apiErr
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, parseError(resp.StatusCode, raw)
		}
		if len(raw) > maxResponseBytes {
			return nil, fmt.Errorf("%s %s: response larger than %d bytes", method, path, maxResponseBytes)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			return json.RawMessage(`{"ok":true}`), nil
		}
		return raw, nil
	}
}

func parseError(status int, raw []byte) *APIError {
	e := &APIError{Status: status}
	var body struct {
		Error   any    `json:"error"`
		Message string `json:"message"`
		Code    string `json:"code"`
		Title   string `json:"title"`
	}
	if json.Unmarshal(raw, &body) == nil {
		e.Code = body.Code
		switch v := body.Error.(type) {
		case string:
			if body.Code == "" && body.Message != "" && !strings.Contains(v, " ") {
				e.Code, e.Message = v, body.Message
			} else {
				e.Message = v
			}
		}
		if e.Message == "" {
			e.Message = firstNonEmpty(body.Message, body.Title)
		}
	}
	if e.Message == "" && len(raw) > 0 && len(raw) < 300 {
		e.Message = strings.TrimSpace(string(raw))
	}
	return e
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// AuthTransport adds a fresh bearer token to every request. The remote MCP
// connection uses it so long sessions survive token renewal.
type AuthTransport struct {
	Client *Client
	Base   http.RoundTripper
}

func (t *AuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := t.Client.Token(r.Context())
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("User-Agent", t.Client.userAgent)
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
