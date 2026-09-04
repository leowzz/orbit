package clock

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestClockAppliesNTPClockOffset(t *testing.T) {
	fixed := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)
	clock, err := New(Config{
		Server: "ntp.example.com", SyncInterval: 10 * time.Minute, Timeout: 2 * time.Second,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	clock.systemNow = func() time.Time { return fixed }
	clock.query = func(server string, timeout time.Duration) (measurement, error) {
		if server != "ntp.example.com" || timeout != 2*time.Second {
			t.Fatalf("query(%q, %v)", server, timeout)
		}
		return measurement{offset: 94 * time.Millisecond, rtt: 8 * time.Millisecond}, nil
	}

	clock.sync()
	if got, want := clock.Now(), fixed.Add(94*time.Millisecond); !got.Equal(want) {
		t.Fatalf("Now() = %v, want %v", got, want)
	}
}

func TestClockKeepsLastOffsetAfterRefreshFailure(t *testing.T) {
	fixed := time.Date(2026, 9, 4, 15, 0, 0, 0, time.UTC)
	clock, err := New(Config{
		Server: "ntp.example.com", SyncInterval: 10 * time.Minute, Timeout: 2 * time.Second,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	clock.systemNow = func() time.Time { return fixed }
	clock.query = func(string, time.Duration) (measurement, error) {
		return measurement{offset: -35 * time.Millisecond}, nil
	}
	clock.sync()
	clock.query = func(string, time.Duration) (measurement, error) {
		return measurement{}, errors.New("timeout")
	}

	clock.sync()
	if got, want := clock.Now(), fixed.Add(-35*time.Millisecond); !got.Equal(want) {
		t.Fatalf("Now() = %v after failed refresh, want %v", got, want)
	}
}
