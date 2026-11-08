#!/usr/bin/env bash
# Runs a build command (its arguments), again when it fails on a download.
#
#   .github/scripts/build-with-retries.sh go tool mygo build -platform linux/amd64 -o dist
#
# mygo build downloads the native libraries of its target from GitHub's
# releases, and gives up on a timeout or a few 5xx within seconds; it ends
# then with
#
#   error: downloading libghostty-vt.so from https://…: …
#
# The files it downloaded before stay in its cache, so another try only
# fetches what is missing. Any other failure ends the script at once.
#
# RETRY_BUDGET_SECONDS (default 600) is how long the script keeps trying,
# RETRY_WAITS (default "15 30 60") the waits between tries, in seconds, the
# last repeating. When the next wait would go past the budget, it gives up
# with the last try's status; that try's output is the last error.
set -euo pipefail

if [ $# -eq 0 ]; then
  echo "usage: $0 command [args...]" >&2
  exit 2
fi

budget=${RETRY_BUDGET_SECONDS:-600}
read -r -a waits <<< "${RETRY_WAITS:-15 30 60}"
log=$(mktemp)
trap 'rm -f "$log"' EXIT

start=$SECONDS
try=1
while :; do
  status=0
  "$@" 2>&1 | tee "$log" || status=$?
  if [ "$status" -eq 0 ]; then
    exit 0
  fi
  # mygo's error line, without the colors FORCE_COLOR would add (tr, as
  # the sed of macOS has no \x1b).
  if ! tr -d '\033' < "$log" | sed 's/\[[0-9;]*m//g' | grep -Eq '^[[:space:]]*error: downloading '; then
    exit "$status"
  fi
  i=$((try - 1))
  if [ "$i" -ge "${#waits[@]}" ]; then
    i=$((${#waits[@]} - 1))
  fi
  wait=${waits[$i]}
  if [ $((SECONDS - start + wait)) -gt "$budget" ]; then
    echo "::error::The build failed on a download again (tries: $try, $((SECONDS - start)) s); giving up"
    exit "$status"
  fi
  echo "::warning::The build failed on a download (try $try); trying again in $wait s"
  sleep "$wait"
  try=$((try + 1))
done
