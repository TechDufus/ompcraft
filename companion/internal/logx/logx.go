// Package logx is a minimal leveled logger: Printf always, Debugf with -v.
package logx

import (
	"io"
	"log"
)

type Logger struct {
	l       *log.Logger
	verbose bool
}

// New logs to w with microsecond timestamps.
func New(w io.Writer, verbose bool) *Logger {
	return &Logger{l: log.New(w, "", log.LstdFlags|log.Lmicroseconds), verbose: verbose}
}

func (l *Logger) Printf(format string, args ...any) { l.l.Printf(format, args...) }

func (l *Logger) Debugf(format string, args ...any) {
	if l.verbose {
		l.l.Printf(format, args...)
	}
}

// Verbose reports whether Debugf output is enabled.
func (l *Logger) Verbose() bool { return l.verbose }

// Discard returns a logger that drops everything (tests).
func Discard() *Logger { return New(io.Discard, false) }
