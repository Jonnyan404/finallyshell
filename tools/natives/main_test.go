package main

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

// clock is a fake time: sleeping and fetching move it forward.
type clock struct {
	t     time.Time
	slept []time.Duration
}

func (c *clock) now() time.Time { return c.t }

func (c *clock) sleep(d time.Duration) {
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
}

// fakeFetch returns a fetch that takes cost on the clock, fails the first
// fails calls (all of them for fails < 0) with an error naming the call,
// then returns path, and counts the calls.
func fakeFetch(c *clock, cost time.Duration, fails int, path string, calls *int) func() (string, error) {
	return func() (string, error) {
		*calls++
		c.t = c.t.Add(cost)
		if fails < 0 || *calls <= fails {
			return "", fmt.Errorf("GET libghostty-vt-linux-amd64.so: 504 Gateway Timeout (call %d)", *calls)
		}
		return path, nil
	}
}

func newRetrier(c *clock, fetch func() (string, error), budget time.Duration, logged *[]string) retrier {
	return retrier{
		fetch:   fetch,
		backoff: backoff,
		budget:  budget,
		now:     c.now,
		sleep:   c.sleep,
		logf: func(format string, args ...any) {
			if logged != nil {
				*logged = append(*logged, fmt.Sprintf(format, args...))
			}
		},
	}
}

func seconds(s ...int) []time.Duration {
	var out []time.Duration
	for _, n := range s {
		out = append(out, time.Duration(n)*time.Second)
	}
	return out
}

func TestRetrySucceedsAfterFailures(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	var calls int
	var logged []string
	r := newRetrier(c, fakeFetch(c, 0, 2, "/cache/mygo/natives/x/libghostty-vt.so", &calls), budget, &logged)
	path, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if path != "/cache/mygo/natives/x/libghostty-vt.so" {
		t.Errorf("path = %q", path)
	}
	if calls != 3 {
		t.Errorf("fetch called %d times, want 3", calls)
	}
	if want := seconds(2, 4); !slices.Equal(c.slept, want) {
		t.Errorf("slept %v, want %v", c.slept, want)
	}
	if len(logged) != 2 {
		t.Errorf("logged %q, want a line per failure", logged)
	}
}

func TestRetryFirstTry(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	var calls int
	r := newRetrier(c, fakeFetch(c, 0, 0, "lib.so", &calls), budget, nil)
	r.logf = func(string, ...any) { t.Error("logged a success") }
	if _, err := r.run(); err != nil || calls != 1 || len(c.slept) != 0 {
		t.Errorf("err = %v, calls = %d, slept %v; want nil, 1, none", err, calls, c.slept)
	}
}

// With a budget of 60 s and instant tries, the tries start at 0, 2, 6,
// 14, 30 and 60 s; the next would start at 90 s.
func TestRetryBudget(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	var calls int
	r := newRetrier(c, fakeFetch(c, 0, -1, "", &calls), time.Minute, nil)
	path, err := r.run()
	if err == nil || path != "" {
		t.Fatalf("run() = %q, %v; want an error", path, err)
	}
	if calls != 6 {
		t.Errorf("fetch called %d times, want 6", calls)
	}
	if want := seconds(2, 4, 8, 16, 30); !slices.Equal(c.slept, want) {
		t.Errorf("slept %v, want %v", c.slept, want)
	}
}

// With the real budget, tries that take 7 s each (MyGo's own retries)
// start at 0, 9, 20, 35, 58, 95, 132, 169, 206, 243 and 280 s; the next
// would start at 317 s, past five minutes. The error is the last try's.
func TestRetryFailsWithLastError(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	var calls int
	last := errors.New("504 Gateway Timeout")
	fetch := fakeFetch(c, 7*time.Second, -1, "", &calls)
	r := newRetrier(c, func() (string, error) {
		_, err := fetch()
		if calls == 11 {
			return "", last
		}
		return "", err
	}, budget, nil)
	_, err := r.run()
	if !errors.Is(err, last) {
		t.Fatalf("err = %v, want the last try's error", err)
	}
	if calls != 11 {
		t.Errorf("fetch called %d times, want 11", calls)
	}
	if want := seconds(2, 4, 8, 16, 30, 30, 30, 30, 30, 30); !slices.Equal(c.slept, want) {
		t.Errorf("slept %v, want %v", c.slept, want)
	}
	if elapsed := c.t.Sub(time.Unix(0, 0)); elapsed != 287*time.Second {
		t.Errorf("gave up after %s, want 4m47s", elapsed)
	}
}
