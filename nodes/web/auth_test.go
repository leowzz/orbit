package web

import (
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
