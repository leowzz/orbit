package logging

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	dateLayout = "0102"
	timeLayout = "15:04:05.000000"
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiBlue   = "\x1b[34m"
)

var logBufferPool = buffer.NewPool()

// New builds the process logger at the configured level.
func New(level string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("parse logging level %q: %w", level, err)
	}

	core := zapcore.NewCore(newCLIEncoder(stdoutIsTerminal()), zapcore.Lock(os.Stdout), zapLevel)
	return zap.New(core, zap.AddCaller()), nil
}

// cliEncoder owns the fixed human-readable prefix and delegates structured
// field encoding to Zap's JSON encoder.
type cliEncoder struct {
	zapcore.Encoder
	color bool
}

func newCLIEncoder(color bool) zapcore.Encoder {
	config := zap.NewProductionEncoderConfig()
	config.MessageKey = ""
	config.LevelKey = ""
	config.TimeKey = ""
	config.NameKey = ""
	config.CallerKey = ""
	config.FunctionKey = ""
	config.StacktraceKey = ""
	return cliEncoder{Encoder: zapcore.NewJSONEncoder(config), color: color}
}

func (encoder cliEncoder) Clone() zapcore.Encoder {
	return cliEncoder{Encoder: encoder.Encoder.Clone(), color: encoder.color}
}

func (encoder cliEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	encodedFields, err := encoder.Encoder.EncodeEntry(zapcore.Entry{}, fields)
	if err != nil {
		return nil, err
	}
	defer encodedFields.Free()
	encodedFields.TrimNewline()

	line := logBufferPool.Get()
	appendText(line, encoder.color, levelColor(entry.Level), shortLevel(entry.Level))
	line.AppendByte(' ')
	appendText(line, encoder.color, ansiDim, entry.Time.Format(dateLayout))
	line.AppendByte(' ')
	appendText(line, encoder.color, ansiCyan, entry.Time.Format(timeLayout))
	line.AppendByte(' ')
	if entry.Caller.Defined {
		appendText(line, encoder.color, ansiBlue, entry.Caller.TrimmedPath())
	} else {
		appendText(line, encoder.color, ansiBlue, "unknown:0")
	}
	line.AppendByte(' ')
	appendText(line, encoder.color, ansiDim, "|")
	line.AppendByte(' ')
	appendText(line, encoder.color, levelColor(entry.Level), entry.Message)
	if encodedFields.Len() > 2 {
		line.AppendByte(' ')
		appendText(line, encoder.color, ansiDim, encodedFields.String())
	}
	line.AppendByte('\n')
	return line, nil
}

func appendText(line *buffer.Buffer, color bool, style, text string) {
	if color {
		line.AppendString(style)
	}
	line.AppendString(text)
	if color {
		line.AppendString(ansiReset)
	}
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func shortLevel(level zapcore.Level) string {
	switch level {
	case zapcore.DebugLevel:
		return "[D]"
	case zapcore.InfoLevel:
		return "[I]"
	case zapcore.WarnLevel:
		return "[W]"
	case zapcore.ErrorLevel:
		return "[E]"
	case zapcore.DPanicLevel, zapcore.PanicLevel:
		return "[P]"
	case zapcore.FatalLevel:
		return "[F]"
	default:
		return "[?]"
	}
}

func levelColor(level zapcore.Level) string {
	switch level {
	case zapcore.DebugLevel:
		return "\x1b[36m"
	case zapcore.InfoLevel:
		return "\x1b[32m"
	case zapcore.WarnLevel:
		return "\x1b[33m"
	case zapcore.ErrorLevel:
		return "\x1b[31m"
	case zapcore.DPanicLevel, zapcore.PanicLevel:
		return "\x1b[35m"
	case zapcore.FatalLevel:
		return "\x1b[1;31m"
	default:
		return ansiDim
	}
}
