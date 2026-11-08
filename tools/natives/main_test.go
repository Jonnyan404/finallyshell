package main

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

// fake returns a fetch that fails the first fails calls with errors that
// name the call, then returns path, and counts the calls.
func fake(fails int, path string, calls *int) func() (string, error) {
	return func() (string, error) {
		*calls++
		if *calls <= fails {
			return "", fmt.Errorf("GET libghostty-vt-linux-amd64.so: 504 Gateway Timeout (call %d)", *calls)
		}
		return path, nil
	}
}

func TestRetrySucceedsAfterFailures(t *testing.T) {
	var calls int
	var slept []time.Duration
	var logged []string
	path, err := retry(fake(2, "/cache/mygo/natives/x/libghostty-vt.so", &calls), backoff,
		func(d time.Duration) { slept = append(slept, d) },
		func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) })
	if err != nil {
		t.Fatal(err)
	}
	if path != "/cache/mygo/natives/x/libghostty-vt.so" {
		t.Errorf("path = %q", path)
	}
	if calls != 3 {
		t.Errorf("fetch called %d times, want 3", calls)
	}
	if want := []time.Duration{2 * time.Second, 4 * time.Second}; !slices.Equal(slept, want) {
		t.Errorf("slept %v, want %v", slept, want)
	}
	if len(logged) != 2 {
		t.Errorf("logged %q, want a line per failure", logged)
	}
}

func TestRetryFailsWithLastError(t *testing.T) {
	var calls int
	var slept []time.Duration
	last := errors.New("504 Gateway Timeout")
	fetch := func() (string, error) {
		calls++
		if calls == 5 {
			return "", last
		}
		return "", fmt.Errorf("failure %d", calls)
	}
	path, err := retry(fetch, backoff, func(d time.Duration) { slept = append(slept, d) }, func(string, ...any) {})
	if !errors.Is(err, last) {
		t.Fatalf("err = %v, want the last try's error", err)
	}
	if path != "" {
		t.Errorf("path = %q, want none", path)
	}
	if calls != 5 {
		t.Errorf("fetch called %d times, want 5", calls)
	}
	if want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}; !slices.Equal(slept, want) {
		t.Errorf("slept %v, want %v", slept, want)
	}
}

func TestRetryFirstTry(t *testing.T) {
	var calls int
	_, err := retry(fake(0, "lib.so", &calls), backoff,
		func(time.Duration) { t.Error("slept after a success") },
		func(string, ...any) { t.Error("logged a success") })
	if err != nil || calls != 1 {
		t.Errorf("err = %v, calls = %d; want nil, 1", err, calls)
	}
}
