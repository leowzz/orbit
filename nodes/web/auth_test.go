package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthTokenExpiresAndCannotBeReusedWithAnotherPassword(t *testing.T) {
	clock := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	auth := newAuthManager(AuthConfig{Password: "web-secret", SessionTTL: time.Minute})
	auth.now = func() time.Time { return clock }
	token, expiresAt, err := auth.issue()
	if err != nil {
		t.Fatal(err)
	}
	if !expiresAt.Equal(clock.Add(time.Minute)) || !auth.valid(token) {
		t.Fatalf("issued token is not valid until its expiry: token=%q expires_at=%v", token, expiresAt)
	}
	clock = clock.Add(time.Minute)
	if auth.valid(token) {
		t.Fatal("expired token was accepted")
	}

	other := newAuthManager(AuthConfig{Password: "other-secret", SessionTTL: time.Minute})
	other.now = func() time.Time { return clock.Add(-time.Minute) }
	if other.valid(token) {
		t.Fatal("token signed with another password was accepted")
	}
}

func TestSessionsAreRandomInstanceBoundAndRevocable(t *testing.T) {
	cfg := AuthConfig{Password: "same-password", SessionTTL: time.Hour}
	auth := newAuthManager(cfg)
	token, _, _ := auth.issue()
	other, _, _ := auth.issue()
	if token == other || len(token) != 43 || newAuthManager(cfg).valid(token) {
		t.Fatal("session is predictable or valid on another server instance")
	}
	auth.revoke(token)
	if auth.valid(token) || !auth.valid(other) {
		t.Fatal("logout did not revoke exactly one session")
	}
}

func TestLoginBudgetRecoversAfterWindow(t *testing.T) {
	auth := newAuthManager(AuthConfig{})
	now := time.Now()
	auth.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if auth.loginRetry() != 0 {
			t.Fatal("early throttle")
		}
	}
	if auth.loginRetry() <= 0 {
		t.Fatal("missing throttle")
	}
	now = now.Add(time.Minute)
	if auth.loginRetry() != 0 {
		t.Fatal("budget never recovers")
	}
}

func TestProtectedStreamEndsOnExpiryAndLogout(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "expiry", true: "logout"}[revoke], func(t *testing.T) {
			auth := newAuthManager(AuthConfig{Password: "secret", SessionTTL: 150 * time.Millisecond})
			if revoke {
				auth.ttl = time.Hour
			}
			token, _, _ := auth.issue()
			started, done := make(chan struct{}), make(chan struct{})
			handler := auth.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
			}))
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			r := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
			r.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
			go func() { handler.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
			<-started
			if revoke {
				auth.revoke(token)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("stream survived session expiry/logout")
			}
		})
	}
}
