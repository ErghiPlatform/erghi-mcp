package erghi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	b, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"HS256"}`)) + "." + enc(b) + ".sig"
}

type fakeAPI struct {
	exchanges atomic.Int32
	calls     atomic.Int32
	// failFirst makes the first API call answer 401 with this code.
	failFirst string
	token     string
}

func newFakeAPI(t *testing.T, permission any) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{token: fakeJWT(t, map[string]any{
		"sub": "cf_client_live_x", "workspace_id": "ws-1", "permission": permission, "exp": 9999999999,
	})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/token" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["client_secret"] != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f.exchanges.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": f.token, "expires_in": 7200})
			return
		}
		n := f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if n == 1 && f.failFirst != "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "nope", "code": f.failFirst})
			return
		}
		switch r.URL.Path {
		case "/empty":
			w.WriteHeader(http.StatusNoContent)
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "API keys cannot call this endpoint.", "code": "api_key_not_allowed"})
		default:
			_, _ = w.Write([]byte(`{"q":"` + r.URL.Query().Get("q") + `"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func newTestClient(t *testing.T, url, secret string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: url, ClientID: "cf_client_live_x", ClientSecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewRequiresHTTPSExceptLoopback(t *testing.T) {
	cases := map[string]bool{
		"https://api.erghi.ai":  true,
		"http://localhost:5080": true,
		"http://127.0.0.1:5080": true,
		"http://[::1]:5080":     true,
		"http://api.erghi.ai":   false,
		"http://10.0.0.5":       false,
		"ftp://api.erghi.ai":    false,
		"not a url":             false,
	}
	for url, ok := range cases {
		_, err := New(Config{BaseURL: url, ClientID: "a", ClientSecret: "b"})
		if (err == nil) != ok {
			t.Errorf("New(%q) error = %v, want ok=%v", url, err, ok)
		}
	}
	if _, err := New(Config{BaseURL: "https://api.erghi.ai"}); err == nil {
		t.Error("New without credentials should fail")
	}
}

func TestClaimsDecodeOneOrManyScopes(t *testing.T) {
	for _, tc := range []struct {
		permission any
		want       []string
	}{
		{"chat:read", []string{"chat:read"}},
		{[]string{"chat:read", "configure_ai"}, []string{"chat:read", "configure_ai"}},
	} {
		_, srv := newFakeAPI(t, tc.permission)
		c := newTestClient(t, srv.URL, "secret")
		claims, err := c.Claims(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(claims.Scopes, ",") != strings.Join(tc.want, ",") || claims.WorkspaceID != "ws-1" {
			t.Errorf("claims = %+v, want scopes %v", claims, tc.want)
		}
	}
}

func TestTokenIsCachedAcrossCalls(t *testing.T) {
	f, srv := newFakeAPI(t, "configure_ai")
	c := newTestClient(t, srv.URL, "secret")
	for range 3 {
		if _, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.exchanges.Load(); got != 1 {
		t.Errorf("exchanges = %d, want 1", got)
	}
}

func TestDoRetriesOnceOnStale401(t *testing.T) {
	f, srv := newFakeAPI(t, "configure_ai")
	f.failFirst = "token_expired"
	c := newTestClient(t, srv.URL, "secret")
	if _, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.exchanges.Load(); got != 2 {
		t.Errorf("exchanges = %d, want 2 (re-exchange after 401)", got)
	}
}

func TestDoDoesNotRetryRevokedKey(t *testing.T) {
	f, srv := newFakeAPI(t, "configure_ai")
	f.failFirst = "api_key_inactive"
	c := newTestClient(t, srv.URL, "secret")
	_, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "api_key_inactive" {
		t.Fatalf("err = %v, want api_key_inactive", err)
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestDoErrorsAndEmptyBodies(t *testing.T) {
	_, srv := newFakeAPI(t, "configure_ai")
	c := newTestClient(t, srv.URL, "secret")

	raw, err := c.Do(context.Background(), http.MethodDelete, "/empty", nil, nil)
	if err != nil || string(raw) != `{"ok":true}` {
		t.Errorf("empty body = %s, %v", raw, err)
	}

	_, err = c.Do(context.Background(), http.MethodGet, "/forbidden", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Code != "api_key_not_allowed" ||
		apiErr.Message != "API keys cannot call this endpoint." {
		t.Errorf("err = %#v", err)
	}
}

func TestBadSecretIsReportedWithoutEchoingIt(t *testing.T) {
	_, srv := newFakeAPI(t, "configure_ai")
	c := newTestClient(t, srv.URL, "wrong-secret-value")
	_, err := c.Claims(context.Background())
	if err == nil || strings.Contains(err.Error(), "wrong-secret-value") {
		t.Fatalf("err = %v", err)
	}
}
