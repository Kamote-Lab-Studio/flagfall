#!/bin/sh
# Proves every sample challenge can be solved: builds and starts the four
# services in Docker, runs each challenge's solve/solve.py, and checks the
# printed flag against challenge.yml. Run from the repo root:
#
#   sh challenges/sample/tools/check-solves.sh
#
# Needs Docker. Leaves nothing running.
set -eu
cd "$(dirname "$0")/.."
export MSYS_NO_PATHCONV=1
here="$(pwd -W 2>/dev/null || pwd)"

services="cookie-jar:8080:41080 template-of-doom:8080:41081 stack-smasher:31337:41337 heap-feng-shui:31338:41338"
cleanup() {
  for s in $services; do docker rm -f "ffcheck-${s%%:*}" >/dev/null 2>&1 || true; done
}
trap cleanup EXIT
for s in $services; do
  name=${s%%:*}; rest=${s#*:}; port=${rest%%:*}; host=${rest#*:}
  docker build -q -t "ffcheck-$name" "$name" >/dev/null
  docker run -d --rm --name "ffcheck-$name" -p "127.0.0.1:$host:$port" "ffcheck-$name" >/dev/null
done
sleep 3

# Sanity Check's flag is printed on the seeded rules page.
sanity=$(sed -n 's/^ *value: "\(flag{[^"]*}\)"$/\1/p' sanity-check/challenge.yml)
if grep -qF "$sanity" ../../internal/seed/data/rules.md; then echo "ok    sanity-check (rules page)"
else echo "FAIL  sanity-check: $sanity is not on the rules page"; exit 1; fi

docker run --rm --add-host host.docker.internal:host-gateway -v "$here:/s" -w /s python:3.12-slim sh -euc '
  apt-get update -qq >/dev/null && apt-get install -y -qq file >/dev/null
  pip install -q --root-user-action=ignore cysignals fpylll pillow 2>/dev/null
  H=host.docker.internal
  fail=0
  flag_of() { sed -n "s/^ *value: \"\(flag{[^\"]*}\)\"$/\1/p" "$1/challenge.yml"; }
  check() {  # check <challenge> <output>
    want=$(flag_of "$1")
    if printf "%s" "$2" | grep -qF "$want"; then echo "ok    $1"; else echo "FAIL  $1: wanted $want, got:"; printf "%s\n" "$2" | tail -3; fail=1; fi
  }
  for c in baby-rsa lattice-whisper packet-archaeology memory-lane where-is-the-server obfuscated-oracle; do
    check $c "$(python $c/solve/solve.py 2>&1 || true)"
  done
  # Reversing: the solved serial and flag must work on the real binaries.
  chmod +x crackme-v3/dist/crackme obfuscated-oracle/dist/oracle
  check crackme-v3 "$(python crackme-v3/solve/solve.py | crackme-v3/dist/crackme)"
  (cd obfuscated-oracle/dist && echo "flag{lifted_the_bytecode}" | ./oracle check.vm | grep -q "nods") || { echo "FAIL  oracle rejects the flag"; fail=1; }
  check cookie-jar "$(python cookie-jar/solve/solve.py $H 41080 2>&1 || true)"
  check template-of-doom "$(python template-of-doom/solve/solve.py $H 41081 2>&1 || true)"
  check stack-smasher "$(python stack-smasher/solve/solve.py $H 41337 2>&1 || true)"
  check heap-feng-shui "$(python heap-feng-shui/solve/solve.py $H 41338 2>&1 || true)"
  # Heap Feng Shui promises every protection.
  file heap-feng-shui/dist/notes | grep -q "pie executable" || { echo "FAIL  notes is not PIE"; fail=1; }
  exit $fail
'
