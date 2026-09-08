package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlowHealthyCompletionDoesNotFallBack(t *testing.T) {
	var fallback atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer fallback-key" {
			fallback.Add(1)
			fmt.Fprint(w, "wrong provider")
			return
		}
		// Reproduce a healthy non-streaming completion beyond the old 10s cap.
		select {
		case <-time.After(11 * time.Second):
			fmt.Fprint(w, "healthy primary")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, provider, err := providerRequest(ctx, Config{FallbackEnabled: true, BaseURL: server.URL, DevPassURL: server.URL, DevPassAPIKey: "fallback-key"}, http.MethodPost, "chat/completions", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "healthy primary" || provider != "LiteLLM" || fallback.Load() != 0 {
		t.Fatalf("body=%q provider=%s fallback=%d err=%v", body, provider, fallback.Load(), err)
	}
}

func TestProviderFailuresRetainBothCauses(t *testing.T) {
	for _, status := range []int{401, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "primary-secret", 503) }))
			defer primary.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "fallback-secret", status) }))
			defer fallback.Close()
			resp, provider, err := providerRequest(context.Background(), Config{FallbackEnabled: true, BaseURL: primary.URL, DevPassURL: fallback.URL, DevPassAPIKey: "key"}, http.MethodGet, "models", nil)
			if resp != nil || err == nil || provider != "DevPass" {
				t.Fatalf("resp=%v provider=%s err=%v", resp, provider, err)
			}
			if !strings.Contains(err.Error(), "LiteLLM returned HTTP 503") || !strings.Contains(err.Error(), fmt.Sprintf("DevPass returned HTTP %d", status)) || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
	_, provider, err := providerRequest(context.Background(), Config{FallbackEnabled: true, BaseURL: ":bad", DevPassURL: ":bad", DevPassAPIKey: "key"}, http.MethodGet, "models", nil)
	if err == nil || provider != "DevPass" || !strings.Contains(err.Error(), "RALPH_DEVPASS_URL") || !strings.Contains(err.Error(), "RALPH_LITELLM_URL") {
		t.Fatalf("provider=%s err=%v", provider, err)
	}
}

func TestProviderHTTPRetryAndCredentialIsolation(t *testing.T) {
	for _, status := range []int{200, 301, 302, 307, 308, 400, 401, 403, 408, 429, 500, 502, 503, 504, 599} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var primaryCalls, fallbackCalls, redirects atomic.Int32
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirects.Add(1) }))
			defer redirect.Close()
			const payload = `{"messages":[{"role":"user","content":"fixture only"}]}`
			check := func(r *http.Request, key string) {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != payload || r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
					t.Error("retry changed request")
				}
				if r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("credential crossed providers")
				}
			}
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				primaryCalls.Add(1)
				check(r, "primary-key")
				w.Header().Set("Location", redirect.URL)
				w.WriteHeader(status)
				fmt.Fprint(w, "primary")
			}))
			defer primary.Close()
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls.Add(1)
				check(r, "fallback-key")
				fmt.Fprint(w, "fallback")
			}))
			defer fallback.Close()
			resp, provider, err := providerRequest(context.Background(), Config{FallbackEnabled: true, BaseURL: primary.URL + "/v1", APIKey: "primary-key", DevPassURL: fallback.URL + "/v1", DevPassAPIKey: "fallback-key"}, http.MethodPost, "chat/completions", []byte(payload))
			if resp != nil {
				resp.Body.Close()
			}
			wantFallback := int32(0)
			if retryableProviderStatus(status) {
				wantFallback = 1
			}
			if primaryCalls.Load() != 1 || fallbackCalls.Load() != wantFallback || redirects.Load() != 0 {
				t.Fatalf("primary=%d fallback=%d redirects=%d", primaryCalls.Load(), fallbackCalls.Load(), redirects.Load())
			}
			if status == 200 || wantFallback == 1 {
				if err != nil || resp == nil {
					t.Fatalf("success failed: %v", err)
				}
			} else if err == nil {
				t.Fatal("non-retryable error succeeded")
			}
			if wantFallback == 1 && provider != "DevPass" {
				t.Fatal("wrong provider attribution")
			}
		})
	}
}

func TestProviderTransportFailureFallsBack(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "recovered") }))
	defer fallback.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, provider, err := providerRequest(ctx, Config{FallbackEnabled: true, BaseURL: primary.URL, DevPassURL: fallback.URL, DevPassAPIKey: "fixture"}, http.MethodGet, "models", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "recovered" || provider != "DevPass" {
		t.Fatalf("body=%s provider=%s err=%v", body, provider, err)
	}
}

func TestHungPrimaryReservesFallbackTime(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "recovered") }))
	defer fallback.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	resp, provider, err := providerRequest(ctx, Config{FallbackEnabled: true, BaseURL: primary.URL, DevPassURL: fallback.URL, DevPassAPIKey: "fixture"}, http.MethodPost, "chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if provider != "DevPass" || ctx.Err() != nil {
		t.Fatal("primary exhausted the fallback budget")
	}
}

func TestFallbackRequiresOptInAndDestination(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OPENCODE2_ROOT", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("DEVPASS_API_KEY", "ambient-fixture")
	t.Setenv("RALPH_FALLBACK_PROVIDER", "")
	t.Setenv("RALPH_DEVPASS_URL", "http://fixture.invalid/v1")
	t.Setenv("RALPH_FALLBACK_MODEL", "devpass/fixture")
	c := loadConfig()
	if len(promptProviders(c)) != 1 || c.fallbackModel("litellm/fixture") != "" {
		t.Fatal("ambient key or model enabled egress")
	}
	t.Setenv("RALPH_FALLBACK_PROVIDER", "devpass")
	t.Setenv("RALPH_DEVPASS_URL", "")
	c = loadConfig()
	if len(promptProviders(c)) != 1 || c.DevPassURL != "" {
		t.Fatal("hardcoded destination enabled")
	}
	t.Setenv("RALPH_DEVPASS_URL", "https://user:secret@fixture.invalid/v1?key=secret")
	c = loadConfig()
	if len(promptProviders(c)) != 2 || c.fallbackDestination() != "https://fixture.invalid/v1" {
		t.Fatal("opt-in failed or doctor leaks secrets")
	}
}

func TestRealRunnerOutageTranscriptAndDecorations(t *testing.T) {
	// Captured from opencode2 v0.0.0-beta-19296 with an isolated config,
	// dummy key and closed loopback port. Only ANSI colour was removed.
	data, err := os.ReadFile("testdata/opencode2-connection-refused.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{string(data), "✗ Error: HTTP 503\nSession summary", "[ERROR] HTTP 503\nShutdown complete"} {
		if !providerUnavailable(errors.New("exit status 1"), output) {
			t.Fatalf("unrecognised diagnostic: %q", output)
		}
	}
	for _, remaining := range []time.Duration{time.Millisecond, time.Second, 19 * time.Second} {
		ctx, cancel := context.WithTimeout(context.Background(), remaining)
		if retryBudgetAvailable(ctx, time.Minute) {
			t.Fatal("retry with unusable budget")
		}
		cancel()
	}
}

func TestLaunchRejectsUnknownFallbackBeforeCreatingRun(t *testing.T) {
	r := fixtureRun(t, `if [ "$1" = models ]; then echo devpass/other; exit 0; fi
touch "__RUN_DIR__/should-not-start"`, 1)
	c := Config{FallbackEnabled: true, StateDir: filepath.Join(t.TempDir(), "new-state"), Runner: r.Runner, FallbackModel: "devpass/missing"}
	_, err := launchRun(c, r.Repo, "Fixture only", RunOptions{Model: "litellm/fixture", Max: 1, Timeout: 1})
	if err == nil || !strings.Contains(err.Error(), "cannot resolve fallback") {
		t.Fatalf("missing fallback accepted: %v", err)
	}
	if _, err := os.Stat(c.StateDir); !os.IsNotExist(err) {
		t.Fatal("invalid route persisted a run")
	}
}

func TestWorkerPreservesBothAttemptErrors(t *testing.T) {
	r := fixtureRun(t, `if [ "$5" = fake/test ]; then echo 'Error: HTTP 503' >&2; else echo 'Error: unknown model' >&2; fi
exit 1`, 1)
	r.FallbackModel = "devpass/missing"
	if err := writeJSON(filepath.Join(r.Dir, "run.json"), r); err != nil {
		t.Fatal(err)
	}
	err := worker(r.Dir)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || !strings.Contains(err.Error(), "devpass/missing") {
		t.Fatalf("primary lost: %v", err)
	}
}

func TestProviderCancellationNeverFallsBack(t *testing.T) {
	for _, preCancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(preCancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				cancel()
				<-r.Context().Done()
			}))
			defer server.Close()
			if preCancelled {
				cancel()
			}
			_, _, err := providerRequest(ctx, Config{FallbackEnabled: true, BaseURL: server.URL, DevPassURL: server.URL, DevPassAPIKey: "key"}, http.MethodPost, "chat/completions", nil)
			want := int32(1)
			if preCancelled {
				want = 0
			}
			if !errors.Is(err, context.Canceled) || calls.Load() != want {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestFallbackUsesSelectedModelAndExplicitOverride(t *testing.T) {
	for _, tc := range []struct{ selected, override, key, want string }{
		{"litellm/selected", "", "key", "devpass/selected"},
		{"anthropic/claude-x", "", "key", ""},
		{"devpass/model", "", "key", ""},
		{"bare-model", "", "key", ""},
		{"litellm/", "", "key", ""},
		{"litellm/selected", "", "", ""},
		{"anthropic/claude-x", "devpass/explicit", "", "devpass/explicit"},
	} {
		c := Config{FallbackEnabled: true, DevPassURL: "http://fixture.invalid", Model: "litellm/default", FallbackModel: tc.override, DevPassAPIKey: tc.key}
		if got := c.fallbackModel(tc.selected); got != tc.want {
			t.Errorf("%+v: got %q", tc, got)
		}
	}
	root := t.TempDir()
	for _, key := range []string{"DEVPASS_API_KEY", "RALPH_FALLBACK_MODEL", "RALPH_MODEL"} {
		t.Setenv(key, "")
	}
	t.Setenv("OPENCODE2_ROOT", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("RALPH_DEVPASS_URL", "http://127.0.0.1:1234/v1")
	c := loadConfig()
	if c.DevPassURL != "http://127.0.0.1:1234/v1" || c.FallbackModel != "" {
		t.Fatalf("URL override or explicit fallback lost")
	}
}

func TestDiagnosticTailIsBoundedAndIgnoresTranscript(t *testing.T) {
	var tail diagnosticTail
	for _, part := range []string{strings.Repeat("Error: HTTP 503\n", 200000), "compile", " error\n"} {
		n, err := tail.Write([]byte(part))
		if n != len(part) || err != nil || len(tail.data) > 4096 {
			t.Fatal("tail failed to bound output")
		}
	}
	if providerUnavailable(errors.New("exit status 1"), tail.String()) {
		t.Fatal("transcript misclassified")
	}
	for _, text := range []string{"Error: HTTP 503\nError: tests failed", "repository contains connection refused", "HTTP 503", "Error: HTTP 401"} {
		if providerUnavailable(errors.New("exit status 1"), text) {
			t.Fatalf("false outage: %q", text)
		}
	}
	if !providerUnavailable(errors.New("exit status 1"), "tool output\n\x1b[31mError: HTTP 503\x1b[0m\n") {
		t.Fatal("terminal diagnostic missed")
	}
	if providerUnavailable(nil, "Error: HTTP 503") {
		t.Fatal("successful iteration retried")
	}
}

func TestRetryRespectsTotalIterationDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if canRetryProvider(ctx, "litellm/model", "devpass/model", errors.New("exit status 1"), "Error: HTTP 503") {
		t.Fatal("expired iteration retried")
	}
	if !canRetryProvider(context.Background(), "litellm/model", "devpass/model", errors.New("exit status 1"), "Error: HTTP 503") {
		t.Fatal("eligible retry refused")
	}
}

func TestE2EWorkerStopBeforeFallback(t *testing.T) {
	for _, control := range []string{"STOP", "ABORT"} {
		t.Run(control, func(t *testing.T) {
			r := fixtureRun(t, `echo attempt >> "__RUN_DIR__/attempts"
touch "__RUN_DIR__/`+control+`"
echo 'Error: HTTP 503' >&2
exit 1`, 1)
			r.FallbackModel = "devpass/model"
			if err := writeJSON(filepath.Join(r.Dir, "run.json"), r); err != nil {
				t.Fatal(err)
			}
			if err := worker(r.Dir); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(r.Dir, "attempts"))
			if err != nil || string(data) != "attempt\n" {
				t.Fatalf("retried after %s: %q, %v", control, data, err)
			}
			var got Run
			if err := readJSON(filepath.Join(r.Dir, "run.json"), &got); err != nil {
				t.Fatal(err)
			}
			want := "stopped"
			if control == "ABORT" {
				want = "aborted"
			}
			if got.Status != want {
				t.Fatalf("state=%s", got.Status)
			}
		})
	}
}

func TestE2EWorkerTranscriptDoesNotTriggerFallback(t *testing.T) {
	r := fixtureRun(t, `echo attempt >> "__RUN_DIR__/attempts"
echo 'Error: HTTP 503 from a test fixture'
echo 'Error: tests failed' >&2
exit 1`, 1)
	r.FallbackModel = "devpass/model"
	if err := writeJSON(filepath.Join(r.Dir, "run.json"), r); err != nil {
		t.Fatal(err)
	}
	if err := worker(r.Dir); err == nil {
		t.Fatal("failed agent succeeded")
	}
	data, err := os.ReadFile(filepath.Join(r.Dir, "attempts"))
	if err != nil || string(data) != "attempt\n" {
		t.Fatalf("transcript triggered retry: %q, %v", data, err)
	}
}

func TestE2ELaunchSelectedModelFallbackAndPersistAttribution(t *testing.T) {
	r := fixtureRun(t, `if [ "$1" = models ]; then echo devpass/selected; exit 0; fi
echo "$5" >> "__RUN_DIR__/attempts"
if [ "$5" = 'litellm/selected' ]; then
  printf '{"token":"%s","iteration":%s}' "$RALPH_COMPLETION_TOKEN" "$RALPH_ITERATION" > .ralph-complete.json
  echo 'Error: HTTP 503' >&2
  exit 1
fi
echo 'Fallback ran without claiming completion'`, 1)
	c := Config{FallbackEnabled: true, DevPassURL: "http://fixture.invalid", StateDir: filepath.Dir(filepath.Dir(r.Dir)), Runner: r.Runner, Model: "litellm/default", DevPassAPIKey: "fixture-key"}
	launched, err := launchRun(c, r.Repo, "Fixture prompt", RunOptions{Model: "litellm/selected", Max: 1, Timeout: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = requestAbort(launched) })
	deadline := time.After(10 * time.Second)
	var got Run
	for {
		if err := readJSON(filepath.Join(launched.Dir, "run.json"), &got); err != nil {
			t.Fatal(err)
		}
		if !got.active() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("detached worker did not finish")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if got.Model != "litellm/selected" || got.FallbackModel != "devpass/selected" || got.ActiveModel != "devpass/selected" || got.Status != "budget reached" {
		t.Fatalf("state=%+v", got)
	}
	data, err := os.ReadFile(filepath.Join(r.Dir, "attempts"))
	if err != nil || string(data) != "litellm/selected\ndevpass/selected\n" {
		t.Fatalf("models=%q, err=%v", data, err)
	}
	log, err := os.ReadFile(filepath.Join(launched.Dir, "iteration-01.log"))
	if err != nil || !strings.Contains(string(log), "retrying iteration with devpass/selected") {
		t.Fatalf("missing retry log: %s, %v", log, err)
	}
}
