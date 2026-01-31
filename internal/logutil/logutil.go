package logutil

import (
	"io"
	"log"
	"os"
	"time"
)

// New builds a logger with millisecond timestamps and the given prefix.
// If out is nil, logs are written to stdout.
func New(prefix string, out io.Writer) *log.Logger {
	if out == nil {
		out = os.Stdout
	}
	return log.New(millisecondWriter{w: out, prefix: prefix}, "", 0)
}

type millisecondWriter struct {
	w      io.Writer
	prefix string
}

func (mw millisecondWriter) Write(p []byte) (int, error) {
	ts := time.Now().Format("2006/01/02 15:04:05.000")

	buf := make([]byte, 0, len(ts)+1+len(mw.prefix)+len(p))
	buf = append(buf, ts...)
	buf = append(buf, ' ')
	if mw.prefix != "" {
		buf = append(buf, mw.prefix...)
	}
	buf = append(buf, p...)

	return mw.w.Write(buf)
}
