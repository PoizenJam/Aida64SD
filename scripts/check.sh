#!/usr/bin/env bash
# Everything CI runs on a push or pull request. Needs Go, Node, mingw-w64
# (x86_64-w64-mingw32-gcc, for the cgo bridge) and the Elgato CLI
# (npm install -g @elgato/cli).
set -euo pipefail
cd "$(dirname "$0")/.."

D=com.poizenjam.aida64sd.sdPlugin

echo "== go vet (native + windows/cgo)"
make vet

echo "== go test"
go test -race -count=1 ./...

echo "== PI scripts"
for f in "$D"/*.js; do node --check "$f"; done
node pi_test/settings_pi_test.js          "$D/pi_utils.js" "$D/settings_pi.js"
node pi_test/global_settings_echo_test.js "$D/pi_utils.js" "$D/settings_pi.js"
node pi_test/refresh_controls_test.js     "$D/pi_utils.js"
node pi_test/sensor_pi_smoke_test.js      "$D/pi_utils.js" "$D/index_pi.js" > /dev/null
node pi_test/dial_pi_test.js              "$D/pi_utils.js" "$D/dial_pi.js"
node pi_test/slot_pi_test.js composite    "$D/pi_utils.js" "$D/slot_pi.js" "$D/composite_pi.js"
node pi_test/slot_pi_test.js derived      "$D/pi_utils.js" "$D/slot_pi.js" "$D/derived_pi.js"
node pi_test/version_sync_test.js         "$D"

echo "== windows build"
make build

# Validate a copy with the binaries in place: validating the source folder
# fails because CodePath (aida64.exe) is not checked in.
echo "== streamdeck validate"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cp -r "$D" "$tmp/"
cp build/aida64.exe build/aida64-bridge.exe "$tmp/$D/"
streamdeck validate "$tmp/$D"

echo "== all checks passed"
