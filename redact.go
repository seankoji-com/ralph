package main

import (
	"bytes"
	"io"
	"regexp"
	"slices"
	"sync"
)

var (
	urlCredentials = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@]+@`)
	bearerToken    = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	// bearerPrefix also matches a token still arriving, so streaming can hold it back.
	bearerPrefix = regexp.MustCompile(`(?i)\bbearer\s*[A-Za-z0-9._~+/=-]*`)
	secretsMu    sync.RWMutex
	secrets      [][]byte
)

// registerSecrets adds configured key values to every later redaction. Short
// values are ignored so ordinary words are never masked.
func registerSecrets(values ...string) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	for _, v := range values {
		if len(v) >= 8 && !slices.ContainsFunc(secrets, func(s []byte) bool { return string(s) == v }) {
			secrets = append(secrets, []byte(v))
		}
	}
}

func maskSecrets(b []byte) []byte {
	b = bearerToken.ReplaceAll(b, []byte("${1}[redacted]"))
	secretsMu.RLock()
	defer secretsMu.RUnlock()
	for _, secret := range secrets {
		b = bytes.ReplaceAll(b, secret, []byte("[redacted]"))
	}
	return b
}

// plainCut returns how much of p can be written now without splitting a
// secret, a bearer token or a "://" that may continue in the next write.
func plainCut(p []byte) int {
	hold := 5 // len("bearer") - 1, which also covers a split "://"
	secretsMu.RLock()
	var spans [][]int
	for _, secret := range secrets {
		hold = max(hold, len(secret)-1)
		for i := 0; ; {
			j := bytes.Index(p[i:], secret)
			if j < 0 {
				break
			}
			spans = append(spans, []int{i + j, i + j + len(secret)})
			i += j + 1
		}
	}
	secretsMu.RUnlock()
	spans = append(spans, bearerPrefix.FindAllIndex(p, -1)...)
	cut := max(0, len(p)-hold)
	for moved := true; moved; {
		moved = false
		for _, span := range spans {
			if span[0] < cut && span[1] >= cut {
				cut, moved = span[0], true
			}
		}
	}
	if cut == 0 && len(p) > 64*1024 {
		return len(p) // never buffer an endless token; maskSecrets still hides its head
	}
	return cut
}

func redactCredentials(s string) string {
	return string(maskSecrets([]byte(urlCredentials.ReplaceAllString(s, "${1}[redacted]@"))))
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
	write := func(b []byte) error { _, err := w.dst.Write(maskSecrets(b)); return err }
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
			count = plainCut(w.pending)
		}
		if err := write(w.pending[:count]); err != nil {
			return err
		}
		w.pending = append([]byte(nil), w.pending[count:]...)
		return nil
	}
	return nil
}
