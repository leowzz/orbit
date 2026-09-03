package logging

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

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
