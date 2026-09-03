package logging

import (
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestCLIEncoderFormat(t *testing.T) {
	t.Parallel()

	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    time.Date(2026, time.September, 3, 17, 14, 2, 610424000, time.Local),
		Caller:  zapcore.EntryCaller{Defined: true, File: "/app/handler/event_handler.go", Line: 110},
		Message: "stopping event handler worker pool",
	}
	encoded, err := newCLIEncoder(false).EncodeEntry(entry, []zapcore.Field{zap.String("worker_pool", "events")})
	if err != nil {
		t.Fatal(err)
	}
	defer encoded.Free()

	want := "[I] 0903 17:14:02.610424 handler/event_handler.go:110 | stopping event handler worker pool {\"worker_pool\":\"events\"}\n"
	if got := encoded.String(); got != want {
		t.Fatalf("encoded log = %q, want %q", got, want)
	}
}

func TestCLIEncoderColorFormat(t *testing.T) {
	t.Parallel()

	entry := zapcore.Entry{
		Level:   zapcore.WarnLevel,
		Time:    time.Date(2026, time.September, 3, 17, 14, 2, 610424000, time.Local),
		Caller:  zapcore.EntryCaller{Defined: true, File: "/app/handler/event_handler.go", Line: 110},
		Message: "stopping event handler worker pool",
	}
	encoded, err := newCLIEncoder(true).EncodeEntry(entry, []zapcore.Field{zap.String("worker_pool", "events")})
	if err != nil {
		t.Fatal(err)
	}
	defer encoded.Free()

	want := "\x1b[33m[W]\x1b[0m \x1b[2m0903\x1b[0m \x1b[36m17:14:02.610424\x1b[0m \x1b[34mhandler/event_handler.go:110\x1b[0m \x1b[2m|\x1b[0m \x1b[33mstopping event handler worker pool\x1b[0m \x1b[2m{\"worker_pool\":\"events\"}\x1b[0m\n"
	if got := encoded.String(); got != want {
		t.Fatalf("encoded log = %q, want %q", got, want)
	}
}

func TestNewAppliesConfiguredLevel(t *testing.T) {
	t.Parallel()

	logger, err := New("warn")
	if err != nil {
		t.Fatal(err)
	}
	if logger.Core().Enabled(zapcore.InfoLevel) {
		t.Fatal("warn logger enabled info logs")
	}
	if !logger.Core().Enabled(zapcore.WarnLevel) {
		t.Fatal("warn logger disabled warn logs")
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	t.Parallel()

	if _, err := New("verbose"); err == nil {
		t.Fatal("New accepted an invalid log level")
	}
}
