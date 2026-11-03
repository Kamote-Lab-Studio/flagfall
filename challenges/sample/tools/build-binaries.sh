#!/bin/sh
# Builds the sample challenges' Linux binaries in Docker, so they're the
# same on every machine. Run from the repo root:
#
#   sh challenges/sample/tools/build-binaries.sh
#
# It regenerates the data files first (tools/gen), then writes:
#   crackme-v3/dist/crackme          static, stripped (Alpine, musl)
#   obfuscated-oracle/dist/oracle    static, stripped (Alpine, musl)
#   stack-smasher/dist/smasher       static, no PIE, no canary, symbols kept
#   heap-feng-shui/dist/notes        Ubuntu 24.04 (glibc 2.39), all protections
set -eu

cd "$(dirname "$0")/../../.."
go run ./challenges/sample/tools/gen >/dev/null
cd challenges/sample
mkdir -p crackme-v3/dist obfuscated-oracle/dist stack-smasher/dist heap-feng-shui/dist

# On Git Bash for Windows, stop path mangling of the container paths.
export MSYS_NO_PATHCONV=1
here="$(pwd -W 2>/dev/null || pwd)"

docker run --rm -v "$here:/src" -w /src alpine:3.20 sh -euc '
  apk add --no-cache build-base >/dev/null
  gcc -O2 -static -s -o crackme-v3/dist/crackme crackme-v3/src/crackme.c
  gcc -O2 -static -s -o obfuscated-oracle/dist/oracle obfuscated-oracle/src/oracle.c
  gcc -O0 -static -no-pie -fno-stack-protector -o stack-smasher/dist/smasher stack-smasher/src/smasher.c
'

docker run --rm -v "$here:/src" -w /src ubuntu:24.04 sh -euc '
  apt-get update -qq >/dev/null && apt-get install -y -qq gcc >/dev/null
  gcc -O2 -D_FORTIFY_SOURCE=2 -fstack-protector-strong -fPIE -pie -Wl,-z,relro,-z,now \
    -o heap-feng-shui/dist/notes heap-feng-shui/src/notes.c
  ldd --version | head -1
'
ls -l crackme-v3/dist obfuscated-oracle/dist stack-smasher/dist heap-feng-shui/dist
