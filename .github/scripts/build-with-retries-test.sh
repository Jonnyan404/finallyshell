#!/usr/bin/env bash
# Tests build-with-retries.sh against a fake mygo that prints what
# mygo build prints when a download fails:
#
#   .github/scripts/build-with-retries-test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/build-with-retries.sh"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

# fake <download failures, or "always"> <exit status of the last try>
# counts its tries in $dir/tries.
cat > "$dir/fake" <<'EOF'
#!/usr/bin/env bash
tries=$(( $(cat "$DIR/tries" 2>/dev/null || echo 0) + 1 ))
echo "$tries" > "$DIR/tries"
echo "  • Building finallyshell for linux/amd64"
if [ "$1" = always ] || [ "$tries" -le "$1" ]; then
  echo "  ○ Downloading libghostty-vt.so"
  echo "  ✗ Downloading libghostty-vt.so"
  echo
  # Colored, as with FORCE_COLOR, on the second try.
  if [ "$tries" -eq 2 ]; then e=$'\e[1;31merror:\e[0m'; else e="error:"; fi
  echo "  $e downloading libghostty-vt.so from https://github.com/egoist/mygo/releases/download/libghostty-vt-befcdfd2c3a1/libghostty-vt-linux-amd64.so: Get \"https://release-assets.githubusercontent.com/…\": http2: timeout awaiting response headers" >&2
  exit 1
fi
if [ "$2" -ne 0 ]; then
  echo >&2
  echo "  error: go build: exit status 1" >&2
  echo "    main.go:3:1: syntax error: non-declaration statement outside function body" >&2
  exit "$2"
fi
echo "  ✓ Built finallyshell"
EOF
chmod +x "$dir/fake"

failed=0
# check <name> <want status> <want tries> <budget> <waits> <fake args...>
check() {
  local name=$1 want=$2 tries=$3 budget=$4 waits=$5
  shift 5
  rm -f "$dir/tries"
  local status=0
  DIR=$dir RETRY_BUDGET_SECONDS=$budget RETRY_WAITS=$waits \
    "$script" "$dir/fake" "$@" > "$dir/out" 2>&1 || status=$?
  local got
  got=$(cat "$dir/tries")
  if [ "$status" -ne "$want" ] || [ "$got" -ne "$tries" ]; then
    echo "FAIL $name: status $status, $got tries; want $want, $tries tries"
    sed 's/^/    /' "$dir/out"
    failed=1
  else
    echo "ok   $name"
  fi
}

check "two download errors, then success" 0 3 600 "0" 2 0
check "another error is not retried" 3 1 600 "0" 0 3
check "a download error, then another error" 3 2 600 "0" 1 3
# Waits of 2 s within 3 s: tries at 0 and 2 s, then the next would end
# after the budget (SECONDS counts whole seconds, so 2 s may read as 3).
check "download errors until the budget is spent" 1 2 3 "2" always 0
if ! grep -q 'giving up' "$dir/out" || ! tail -n 1 "$dir/out" | grep -q 'giving up' ||
  ! grep -q 'error: downloading libghostty-vt.so' "$dir/out"; then
  echo "FAIL the output of the last try and the reason to give up"
  sed 's/^/    /' "$dir/out"
  failed=1
fi
# The waits cap at the last one: 0 s, 2 s, then 2 s again, which would end
# after the budget.
check "the last wait repeats" 1 3 3 "0 2" always 0

exit "$failed"
