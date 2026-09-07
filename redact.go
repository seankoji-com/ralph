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

// Stream ordinary output immediately. Hold only a possible URL authority until
// its delimiter tells us whether it contains userinfo, including across writes.
// Extremely long authorities are redacted whole without losing surrounding logs.
type redactingWriter struct {
	dst               io.Writer
	pending           []byte
	authority, hidden bool
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		count := min(len(p), 32*1024)
		w.pending = append(w.pending, p[:count]...)
		p = p[count:]
		if err := w.drain(false); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (w *redactingWriter) Flush() error { return w.drain(true) }

func (w *redactingWriter) drain(final bool) error {
	write := func(b []byte) error { _, err := w.dst.Write(b); return err }
	for len(w.pending) > 0 {
		if w.authority {
			end := bytes.IndexAny(w.pending, " /@\t\r\n")
			if end < 0 {
				if w.hidden {
					w.pending = nil
					return nil
				}
				if len(w.pending) > 64*1024 {
					if err := write([]byte("[redacted oversized URL authority]")); err != nil {
						return err
					}
					w.hidden = true
					w.pending = nil
					return nil
				}
				if !final {
					return nil
				}
				if err := write(w.pending); err != nil {
					return err
				}
				w.pending = nil
				w.authority = false
				return nil
			}
			if !w.hidden {
				text := w.pending[:end]
				if w.pending[end] == '@' {
					text = []byte("[redacted]")
				}
				if err := write(text); err != nil {
					return err
				}
			}
			if err := write(w.pending[end : end+1]); err != nil {
				return err
			}
			w.pending = w.pending[end+1:]
			w.authority = false
			w.hidden = false
			continue
		}
		if start := bytes.Index(w.pending, []byte("://")); start >= 0 {
			if err := write(w.pending[:start+3]); err != nil {
				return err
			}
			w.pending = w.pending[start+3:]
			w.authority = true
			continue
		}
		count := len(w.pending)
		if !final {
			count = max(0, count-2)
		} // a split :// may begin in the last two bytes
		if err := write(w.pending[:count]); err != nil {
			return err
		}
		w.pending = append([]byte(nil), w.pending[count:]...)
		return nil
	}
	return nil
}
