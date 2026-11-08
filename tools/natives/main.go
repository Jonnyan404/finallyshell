// Command natives downloads the native libraries the tests load, so that a
// download that fails in passing is tried again before the tests run. CI
// (.github/workflows/ci.yml) runs it from the repository's root before
// go test:
//
//	go run ./tools/natives
//
// The only library is libghostty-vt, which MyGo's terminal plugin
// downloads into the user's cache (<cache>/mygo/natives/<sha256>/) the
// first time a program that is not a packaged app starts a terminal, and
// checks its SHA-256. MyGo tries a download again three times within about
// seven seconds when GitHub answers 5xx, but GitHub's 504s can go on for
// minutes, and terminal.Load tries only once per process, so a test run
// that starts then fails every test that opens a terminal. This command
// calls terminal.LibraryPath, which finds the library or downloads it,
// again and again for up to five minutes, waiting 2, 4, 8 and 16 seconds,
// then 30 seconds, between tries, and prints its path. When the time is up
// it fails with the last try's error, so a download that keeps failing
// still fails the job.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// backoff is how long to wait after each failed try; the last wait repeats.
var backoff = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second}

// budget is how long to keep trying: no try starts later than this after
// the first.
const budget = 5 * time.Minute

func main() {
	log.SetFlags(0)
	r := retrier{
		fetch:   terminal.LibraryPath,
		backoff: backoff,
		budget:  budget,
		now:     time.Now,
		sleep:   time.Sleep,
		logf:    log.Printf,
	}
	path, err := r.run()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(path)
}

// retrier calls fetch until it succeeds or budget has passed.
type retrier struct {
	fetch   func() (string, error)
	backoff []time.Duration
	budget  time.Duration
	now     func() time.Time
	sleep   func(time.Duration)
	logf    func(format string, args ...any)
}

// run calls fetch until it succeeds, waiting backoff[i] after the i-th
// failure (the last wait for the later ones), as long as the next try
// would start within budget of the first. It logs each failure, and
// returns the path of the first success or the last try's error.
func (r retrier) run() (string, error) {
	start := r.now()
	for try := 1; ; try++ {
		path, err := r.fetch()
		if err == nil {
			return path, nil
		}
		wait := r.backoff[min(try, len(r.backoff))-1]
		if elapsed := r.now().Sub(start); elapsed+wait > r.budget {
			return "", fmt.Errorf("natives: gave up after %d tries in %s: %w", try, elapsed.Round(time.Second), err)
		}
		r.logf("natives: try %d failed, trying again in %s: %v", try, wait, err)
		r.sleep(wait)
	}
}
