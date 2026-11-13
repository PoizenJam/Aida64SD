#!/usr/bin/env bash
# Build the release artifacts and notes for a tag.
#   scripts/release.sh v1.2.3
# Writes release/ (the files to attach) and release-notes.md (the body).
set -euo pipefail
cd "$(dirname "$0")/.."

tag=${1:?usage: scripts/release.sh vX.Y.Z}
[[ $tag =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]] || { echo "tag must look like v1.2.3, got $tag"; exit 1; }
ver=${BASH_REMATCH[1]}

D=com.poizenjam.aida64sd.sdPlugin
manifest_ver=$(node -p "require('./$D/manifest.json').Version")
[[ $manifest_ver == "$ver.0" ]] || { echo "manifest Version is $manifest_ver, tag needs $ver.0"; exit 1; }

# The changelog section for this version is the release body; refuse to
# release without one.
section=$(awk -v v="$ver" '
  $0 ~ "^## " v "([ (]|$)" { on = 1; next }
  on && /^## / { exit }
  on { print }
' CHANGELOG.md)
[[ -n ${section//[[:space:]]/} ]] || { echo "CHANGELOG.md has no '## $ver' section"; exit 1; }

bash scripts/check.sh
make dump package

rm -rf release && mkdir release
mv com.poizenjam.aida64sd.streamDeckPlugin release/
cp build/aida64-bridge.exe build/aida64dump.exe release/
(cd release && sha256sum -- * > SHA256SUMS.txt)

{
  # GitHub renders single newlines in release bodies as line breaks, so
  # unwrap the changelog's hard-wrapped paragraphs and bullets.
  printf '%s\n' "$section" | sed -e '/./,$!d' | awk '
    /^[[:space:]]*$/ { if (buf != "") print buf; buf = ""; print; next }
    buf != "" && !/^(#|-|\*|[0-9]+\.|```)/ { sub(/^[[:space:]]+/, ""); buf = buf " " $0; next }
    { if (buf != "") print buf; buf = $0 }
    END { if (buf != "") print buf; print "" }
  '
  cat <<'EOF'
### Install

1. Install [AIDA64](https://www.aida64.com/) and enable **File > Preferences > Hardware Monitoring > External Applications > Enable shared memory**.
2. Download `com.poizenjam.aida64sd.streamDeckPlugin` below and double-click it. Stream Deck 6.9 or later on Windows 10/11.
3. Drag a **Settings** key onto your deck to see the connection status and set the refresh rate.

The binaries are not code-signed, so Windows SmartScreen may warn on first run.

`aida64-bridge.exe` and `aida64dump.exe` are attached for troubleshooting only; the plugin file already contains the bridge. `aida64dump.exe` prints every sensor AIDA64 is sharing.

### Checksums (SHA-256)

```
EOF
  cat release/SHA256SUMS.txt
  cat <<'EOF'
```

Adapted from [hwinfo-streamdeck](https://github.com/moeilijk/hwinfo-streamdeck) by Shayne Sweeney and moeilijk (GPL-3.0). Not affiliated with FinalWire (AIDA64), Elgato, or the hwinfo-streamdeck authors.
EOF
} > release-notes.md

echo "== release/ and release-notes.md ready for $tag"
ls -l release
