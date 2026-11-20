package gigachat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(Options{AuthKey: "private-key", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func tokenBody(token string) string {
	return fmt.Sprintf(`{"access_token":%q,"expires_at":%d}`, token, time.Now().Add(time.Hour).UnixMilli())
}

const chatBody = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`

func TestOAuthChatAndCachedToken(t *testing.T) {
	c := newTestClient(t)
	authCalls, chatCalls := 0, 0
	c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.Header.Get("Accept") != "application/json" {
			t.Fatal("unexpected method/accept")
		}
		if r.URL.String() == DefaultAuthURL {
			authCalls++
			if r.Header.Get("Authorization") != "Basic private-key" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatal("wrong OAuth headers")
			}
			if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(r.Header.Get("RqUID")) {
				t.Fatal("RqUID is not UUIDv4")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("scope") != DefaultScope {
				t.Fatal("wrong scope")
			}
			return reply(200, tokenBody("access-token")), nil
		}
		chatCalls++
		if r.URL.String() != DefaultBaseURL+"/chat/completions" || r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("wrong chat request")
		}
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != DefaultModel || request.Stream || len(request.Messages) != 1 {
			t.Fatalf("wrong payload: %+v %v", request, err)
		}
		return reply(200, chatBody), nil
	})
	for i := 0; i < 2; i++ {
		response, err := c.Chat(context.Background(), ChatRequest{Model: DefaultModel, Messages: []Message{{Role: "user", Content: "hello"}}})
		if err != nil || len(response.Choices) != 1 || response.Choices[0].Message.Content != "ok" {
			t.Fatalf("response=%+v err=%v", response, err)
		}
	}
	if authCalls != 1 || chatCalls != 2 {
		t.Fatalf("auth=%d chat=%d", authCalls, chatCalls)
	}
}

func TestExpiredAndRejectedTokens(t *testing.T) {
	for _, expired := range []bool{true, false} {
		t.Run(fmt.Sprint("expired=", expired), func(t *testing.T) {
			c := newTestClient(t)
			c.accessToken = "old"
			c.expiresAt = time.Now().Add(time.Hour)
			if expired {
				c.expiresAt = time.Now().Add(-time.Second)
			}
			authCalls, chatCalls := 0, 0
			c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() == DefaultAuthURL {
					authCalls++
					return reply(200, tokenBody("new")), nil
				}
				chatCalls++
				if r.Header.Get("Authorization") == "Bearer old" {
					return reply(401, "private error"), nil
				}
				return reply(200, chatBody), nil
			})
			if _, err := c.Chat(context.Background(), ChatRequest{}); err != nil {
				t.Fatal(err)
			}
			wantCalls := 2
			if expired {
				wantCalls = 1
			}
			if authCalls != 1 || chatCalls != wantCalls {
				t.Fatalf("auth=%d chat=%d", authCalls, chatCalls)
			}
		})
	}
}

func TestConcurrentTokenRefresh(t *testing.T) {
	c := newTestClient(t)
	var authCalls atomic.Int32
	c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == DefaultAuthURL {
			authCalls.Add(1)
			return reply(200, tokenBody("token")), nil
		}
		return reply(200, chatBody), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			if _, err := c.Chat(context.Background(), ChatRequest{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if authCalls.Load() != 1 {
		t.Fatalf("refreshes=%d", authCalls.Load())
	}
}

func TestFailuresAreBoundedAndDoNotLeakResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		auth   bool
	}{
		{"unauthorized", 401, "private-upstream-body", false},
		{"rate limited", 429, "private-upstream-body", false},
		{"server failure", 500, "private-upstream-body", false},
		{"malformed", 200, `{"choices":"private-upstream-body"}`, false},
		{"oversized", 200, strings.Repeat(" ", maxResponseBytes+1), false},
		{"auth failure", 401, "private-upstream-body", true},
		{"empty token", 200, `{"expires_at":9999999999999}`, true},
		{"expired token", 200, `{"access_token":"private-upstream-body","expires_at":1}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			calls := 0
			c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if !tc.auth && r.URL.String() == DefaultAuthURL {
					return reply(200, tokenBody("token")), nil
				}
				return reply(tc.status, tc.body), nil
			})
			_, err := c.Chat(context.Background(), ChatRequest{})
			if err == nil || strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), "private-upstream-body") {
				t.Fatalf("unsafe/missing error: %v", err)
			}
			if tc.status != 200 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status {
					t.Fatalf("err=%v", err)
				}
			}
			want := 2
			if tc.auth {
				want = 1
			} else if tc.status == 401 {
				want = 4
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
		})
	}
}

func TestMissingKeyAndCancellation(t *testing.T) {
	c := newTestClient(t)
	c.authKey = ""
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network request"); return nil, nil })
	if _, err := c.Chat(context.Background(), ChatRequest{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err=%v", err)
	}
	c.authKey = "key"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Chat(ctx, ChatRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	c.tokenLock <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if _, err := c.Chat(ctx, ChatRequest{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	<-c.tokenLock
}

func TestClientOptionsAndRedirects(t *testing.T) {
	for _, opts := range []Options{{BaseURL: "http://insecure.test"}, {AuthURL: "https://user:pass@example.com"}, {BaseURL: "https://example.com?q=key"}, {CAFile: "/nonexistent/ca.pem"}, {Timeout: -time.Second}} {
		if _, err := NewClient(opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
	c := newTestClient(t)
	if err := c.httpClient.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatal("redirects should be disabled")
	}
}
