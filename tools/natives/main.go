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
// seven seconds when GitHub answers 5xx, but GitHub's 504s can last longer,
// and terminal.Load tries only once per process, so a test run that starts
// then fails every test that opens a terminal. This command calls
// terminal.LibraryPath, which finds the library or downloads it, up to
// five times, waiting longer before each new try (30 seconds in all), and
// prints its path. After the last try it fails with that try's error, so a
// download that keeps failing still fails the job.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
)

// backoff is how long to wait before each new try: five tries in all.
var backoff = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}

func main() {
	log.SetFlags(0)
	path, err := retry(terminal.LibraryPath, backoff, time.Sleep, log.Printf)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(path)
}

// retry calls fetch until it succeeds, once more than there are waits in
// backoff, sleeping backoff[i] after the i-th failure, and logging each
// failure with logf. It returns the path of the first success, or the last
// error.
func retry(fetch func() (string, error), backoff []time.Duration, sleep func(time.Duration), logf func(format string, args ...any)) (string, error) {
	tries := len(backoff) + 1
	for try := 1; ; try++ {
		path, err := fetch()
		if err == nil {
			return path, nil
		}
		if try == tries {
			return "", fmt.Errorf("natives: try %d of %d failed: %w", try, tries, err)
		}
		logf("natives: try %d of %d failed, trying again in %s: %v", try, tries, backoff[try-1], err)
		sleep(backoff[try-1])
	}
}
