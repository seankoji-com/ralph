package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactsBearerTokensAndConfiguredKeys(t *testing.T) {
	registerSecrets("fixture-config-key-123", "short")
	got := redactCredentials("Authorization: Bearer abc.def-ghi and key fixture-config-key-123; short stays")
	if strings.Contains(got, "abc.def") || strings.Contains(got, "fixture-config-key") || !strings.Contains(got, "Bearer [redacted]") || !strings.Contains(got, "short stays") {
		t.Fatal(got)
	}
}

func TestStreamingRedactsKeysAndBearerAcrossWrites(t *testing.T) {
	registerSecrets("fixture-stream-key-456")
	input := "curl -H 'Authorization: Bearer tok_fixture_789' then fixture-stream-key-456 done\nplain tail"
	var b bytes.Buffer
	w := &redactingWriter{dst: &b}
	for i := range len(input) {
		if _, err := w.Write([]byte{input[i]}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "tok_fixture") || strings.Contains(out, "fixture-stream-key") || !strings.HasSuffix(out, "done\nplain tail") {
		t.Fatal(out)
	}
}
