package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	DefaultLogger zerolog.Logger
)

type Config struct {
	Level string
	Pretty bool
	WithCaller bool
	TimeFormat string
	Output io.Writer
}

var Levels = map[string]zerolog.Level{
	"debug":    zerolog.DebugLevel,
	"info":     zerolog.InfoLevel,
	"warn":     zerolog.WarnLevel,
	"error":    zerolog.ErrorLevel,
	"fatal":    zerolog.FatalLevel,
	"panic":    zerolog.PanicLevel,
	"disabled": zerolog.Disabled,
}

// InitLogger initializes the global logger with the given configuration
func InitLogger(cfg Config) {
	output := cfg.Output
	if output == nil {
		output = os.Stderr
	}

	timeFormat := cfg.TimeFormat
	if timeFormat == "" {
		timeFormat = time.RFC3339
	}

	zerolog.TimeFieldFormat = timeFormat

	level := zerolog.InfoLevel
	if lvl, ok := Levels[strings.ToLower(cfg.Level)]; ok {
		level = lvl
	}
	zerolog.SetGlobalLevel(level)

	var logger zerolog.Logger
	if cfg.Pretty {
		logger = zerolog.New(zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: timeFormat,
		})
	} else {
		logger = zerolog.New(output)
	}

	logger = logger.With().Timestamp().Logger()

	if cfg.WithCaller {
		logger = logger.With().Caller().Logger()
	}

	DefaultLogger = logger
	log.Logger = logger
}

// GetLogger returns a new logger with the component field set
func GetLogger(component string) zerolog.Logger {
	return DefaultLogger.With().Str("component", component).Logger()
}

// Debug logs a debug message
func Debug(msg string, args ...interface{}) {
	if len(args) > 0 {
		DefaultLogger.Debug().Msgf(msg, args...)
	} else {
		DefaultLogger.Debug().Msg(msg)
	}
}

// Info logs an info message
func Info(msg string, args ...interface{}) {
	if len(args) > 0 {
		DefaultLogger.Info().Msgf(msg, args...)
	} else {
		DefaultLogger.Info().Msg(msg)
	}
}

// Warn logs a warning message
func Warn(msg string, args ...interface{}) {
	if len(args) > 0 {
		DefaultLogger.Warn().Msgf(msg, args...)
	} else {
		DefaultLogger.Warn().Msg(msg)
	}
}

// Error logs an error message
func Error(err error, msg string, args ...interface{}) {
	event := DefaultLogger.Error().Err(err)
	if len(args) > 0 {
		event.Msgf(msg, args...)
	} else {
		event.Msg(msg)
	}
}

// Fatal logs a fatal message and exits
func Fatal(err error, msg string, args ...interface{}) {
	event := DefaultLogger.Fatal().Err(err)
	if len(args) > 0 {
		event.Msgf(msg, args...)
	} else {
		event.Msg(msg)
	}
}

// WithField adds a field to the logger context
func WithField(key string, value interface{}) zerolog.Logger {
	return DefaultLogger.With().Interface(key, value).Logger()
}

// FormatError creates a formatted error string
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%v", err)
}

func init() {
	InitLogger(Config{
		Level:      "info",
		Pretty:     true,
		WithCaller: true,
	})
}
