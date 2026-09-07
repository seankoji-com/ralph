package main

import (
	"bytes"
	"io"
	"regexp"
)

var urlCredentials = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@]+@`)

func redactCredentials(s string) string {
	return urlCredentials.ReplaceAllString(s, "${1}[redacted]@")
}

// Buffer complete lines so a credential split across process writes is still
// redacted before either log receives it. Oversized lines are discarded whole.
type redactingWriter struct {
	dst     io.Writer
	line    []byte
	discard bool
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		end := len(p)
		if i >= 0 {
			end = i + 1
		}
		if !w.discard {
			if len(w.line)+end > 1024*1024 {
				w.line = nil
				w.discard = true
			} else {
				w.line = append(w.line, p[:end]...)
			}
		}
		p = p[end:]
		if i >= 0 {
			if err := w.Flush(); err != nil {
				return 0, err
			}
		}
	}
	return n, nil
}

func (w *redactingWriter) Flush() error {
	s := redactCredentials(string(w.line))
	if w.discard {
		s = "[oversized output line omitted]\n"
	}
	w.line, w.discard = nil, false
	_, err := io.WriteString(w.dst, s)
	return err
}
