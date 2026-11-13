# Development

The host and bridge are Go (version in `go.mod`); the Property Inspectors are
plain JavaScript.

## AIDA64 backend

AIDA64 exposes sensors two ways; this plugin uses the first:

- **Shared memory** `AIDA64_SensorValues`. AIDA64 writes a NUL-terminated
  string of XML-style tags:

  ```
  <temp><id>TCPU</id><label>CPU Package</label><value>52.5</value></temp>
  <volt><id>VCPU</id><label>CPU Core</label><value>1.164</value></volt>
  <fan> <duty> <curr> <pwr> <sys> ...
  ```

  Observed top-level tags: `temp`, `volt`, `fan`, `duty`, `curr`, `pwr` and
  `sys`. Each holds `<id>`, `<label>`, `<value>`. There is no root element;
  each top-level tag is one reading. Temperatures are always Celsius and
  labels are always English, regardless of AIDA64's display settings.
- **Registry** `HKCU\Software\FinalWire\SensorValues` (documented fallback,
  not used).

Unlike HWiNFO, which publishes a packed binary struct with a per-reading
type enum, AIDA64 gives a string with a per-tag category and no unit field,
so classification is category-plus-id based (`internal/aida64/reading.go`):

- the category tag is the primary signal (`temp`/`volt`/`fan`/`duty`/`curr`/`pwr`);
- a label ending in `%` is a percentage regardless of category (e.g. `<pwr>`
  `PGPU1TDPP` "GPU1 TDP%");
- `duty` is a usage/percentage reading;
- `sys` has no unit, so the stable id is the classifier: a regex table
  (`sysRules`) maps ids to clocks (MHz), usage (%), throughput (Mbps for NIC
  connection speed only; KB/s NIC rates, MB/s disk, GB/s memory bandwidth),
  data volume (MB for memory/NIC totals, GB for drive space), FPS counters
  and multipliers. Checked against AIDA64's own sensor-log unit column; on a
  full capture 132 of the 166 `sys` ids classify, and the rest are names,
  IPs, dates and status counters that stay unit-less.

## Architecture

Three processes, as in hwinfo-streamdeck:

1. **aida64-bridge.exe** (cgo) reads `AIDA64_SensorValues` on a fixed 1 s
   ticker, decodes the tags, caches the latest snapshot with a
   content-change freshness token, and serves it over gRPC
   (hashicorp/go-plugin). Four unary RPCs: `PollTime`, `Sensors`,
   `ReadingsForSensorID`, `Sources`. Sensor groups are the AIDA64
   categories; enumeration is dynamic, with no hardcoded sensor list.
2. **aida64.exe**, the Stream Deck plugin host, spawns and supervises the
   bridge and runs one global ticker (default 1 s, 250 ms to 10 s) that
   walks every key: dirty-check against the last-rendered value, pull
   changed readings, render, `SetImage`. A 60 s staleness window on the
   freshness token catches an idle AIDA64 rewriting identical content.
   Bridge calls carry a 2 s deadline, and a bridge that fails three
   consecutive liveness checks is restarted (a closed AIDA64 is not a
   failure).
3. The **Stream Deck WebSocket layer** (`pkg/streamdeck`) connects the host
   to the Stream Deck app and the Property Inspectors.

Host-side behaviours:

- **Auto refresh** reads AIDA64's sensor update rate from AIDA64.INI
  (auto-located, or the user's path and rate key) and applies it, clamped to
  250 ms to 10 s. AIDA64 rewrites the INI when it exits, so the host only
  re-reads it when its modification time changes. If the INI or its rate key
  can't be resolved, the manual interval applies and the Settings PI shows
  why.
- **Auto range** starts each graph from a per-unit scale (Volt 0 to 16, W
  0 to 400, NIC rate 0 to 130 000 KB/s, memory 0 to 64 GB, and so on) and
  grows the max toward the observed high-water mark. A manual Min/Max pins
  the scale.
- **Settings flow.** The host's status push is the only input to the
  refresh and status UI; Property Inspectors send commands and never write
  global settings themselves. Partial saves are merged at the raw-message
  level so one inspector can't clobber another's fields.

AIDA64 shared memory is local-only, so `Sources()` always returns one entry
(id `""`, name "Local") and key settings hold short category-tag strings
rather than source-qualified ids.

**mock-bridge** drives the real bridge service with a random-walking
AIDA64-format block for UI work without AIDA64. It is never shipped.

## Repository layout

- `internal/aida64/`: reading model, classification, tag parser and the
  Windows shared-memory reader. `testdata/` holds two live captures (54 and
  222 readings) pinned by the parser tests.
- `internal/aida64/plugin/`: the bridge core (ticker, snapshot cache,
  freshness token, gRPC plugin).
- `internal/app/aida64plugin/`: the plugin host (the five actions, the
  global ticker, bridge supervision, status push, threshold state machine).
- `pkg/service/`: the `HardwareService` interface and gRPC glue;
  `pkg/service/proto/` the `.proto` and generated code.
- `pkg/graph/`, `pkg/streamdeck/`: graph rendering and the Stream Deck
  WebSocket layer.
- `cmd/`: entry points for `aida64` (host), `aida64-bridge`, `aida64dump`
  (prints what AIDA64 is sharing) and `mock-bridge`.
- `com.poizenjam.aida64sd.sdPlugin/`: the plugin folder (manifest, Property
  Inspectors, icons, css, font).
- `art/`: icon and Marketplace art generators (`make_icons.py` needs
  `rsvg-convert`; `make_gallery.py` needs Pillow) and their outputs.
- `com.poizenjam.aida64sd.sdPlugin/*_pi.js`: one script per Property
  Inspector. `pi_utils.js` (loaded first by all of them) holds the shared
  socket, source filter, sensor picker and snooze helpers; `slot_pi.js` the
  slot pickers shared by Composite and Derived.
- `pi_test/`: Node tests that run the real Property Inspector scripts
  against a DOM stub.
- `scripts/`: `check.sh` (everything CI runs) and `release.sh`.

## Build on Windows

```powershell
powershell -ExecutionPolicy Bypass -File .\build.ps1            # build only
powershell -ExecutionPolicy Bypass -File .\build.ps1 -Install   # build, then install into Stream Deck
```

`build.ps1` compiles the host and assembles the plugin folder in `dist\`.
With `-Install` it closes Stream Deck, replaces the installed plugin and
restarts Stream Deck; it installs nothing if the build fails.

The bridge needs a C compiler for cgo. If `gcc` is on the PATH (for example
[MSYS2](https://www.msys2.org) with `pacman -S mingw-w64-x86_64-gcc`),
`build.ps1` builds it too; otherwise it uses `build\aida64-bridge.exe`,
which must match the source version (download it from the matching
release).

`build.ps1` does not make a `.streamDeckPlugin`. For that, install Elgato's
CLI (`npm install -g @elgato/cli`) and run
`streamdeck pack dist\com.poizenjam.aida64sd.sdPlugin`.

## Build from Linux/WSL

Needs Go, mingw-w64 (`x86_64-w64-mingw32-gcc`) for the cgo binaries, and
Elgato's CLI for packaging.

```sh
make build    # build/aida64.exe, aida64-bridge.exe, aida64dump.exe, mock-bridge.exe
make vet      # native + Windows cross-compile
make test     # Go tests
make package  # com.poizenjam.aida64sd.streamDeckPlugin via streamdeck pack
```

A plain zip of the plugin folder is not a valid `.streamDeckPlugin`; use
`streamdeck pack`. `.sdignore` keeps the mock bridge, aida64dump and logs
out of the package. Builds use `-trimpath` and strip debug info, so the
exes carry no build-machine paths.

## Tests

```sh
go test -race ./...
D=com.poizenjam.aida64sd.sdPlugin
node pi_test/settings_pi_test.js          $D/pi_utils.js $D/settings_pi.js
node pi_test/global_settings_echo_test.js $D/pi_utils.js $D/settings_pi.js
node pi_test/refresh_controls_test.js     $D/pi_utils.js
node pi_test/sensor_pi_smoke_test.js      $D/pi_utils.js $D/index_pi.js > /dev/null
node pi_test/dial_pi_test.js              $D/pi_utils.js $D/dial_pi.js
node pi_test/slot_pi_test.js composite    $D/pi_utils.js $D/slot_pi.js $D/composite_pi.js
node pi_test/slot_pi_test.js derived      $D/pi_utils.js $D/slot_pi.js $D/derived_pi.js
node pi_test/version_sync_test.js         $D
```

`scripts/check.sh` runs all of these plus `go vet`, the Windows build and
`streamdeck validate`. CI runs it on every push to main and every pull
request.

`version_sync_test.js` fails unless the manifest Version, `hostVersion` in
`internal/app/aida64plugin/version.go`, `PI_VERSION` in `pi_utils.js` and
the `?v=` cache busters on the PI script tags all match.

## Releasing

1. In one commit, bump the version everywhere `version_sync_test.js` checks
   and add a `## X.Y.Z` section to `CHANGELOG.md`.
2. Push the commit, then the tag: `git tag vX.Y.Z`, `git push origin vX.Y.Z`.
3. The Release workflow builds the plugin, attaches it with the bridge,
   aida64dump and SHA-256 checksums, and opens a draft release whose notes
   are that changelog section plus install steps. Review it and publish.

To rebuild a draft (for example after a failed run), use Actions > Release
> Run workflow with the tag. It replaces the draft's files and notes; a
published release is never replaced. A tag that must point at a newer commit
has to be moved first (`git tag -f vX.Y.Z`, `git push -f origin vX.Y.Z`),
which is only reasonable while the release is still a draft.

`scripts/release.sh vX.Y.Z` does the same build locally and writes
`release/` and `release-notes.md`.

## Dependency checks

Dependabot opens weekly PRs for Go module and GitHub Actions updates
(`.github/dependabot.yml`), and the govulncheck workflow checks the Windows
build against the Go vulnerability database weekly and on every push to
main.

## Checking against a real AIDA64

The automated tests can't run AIDA64 or Stream Deck. With AIDA64 running and
shared memory enabled:

```powershell
.\build\aida64dump.exe          # category / id / label / value
.\build\aida64dump.exe -raw     # the raw shared-memory string
.\build\aida64-bridge.exe       # prints the go-plugin handshake line if it starts
```

A `-raw` capture can be added to `internal/aida64/testdata/` to pin the
parser against new hardware. For the full check, install the plugin, add a
Settings key (Status shows connection, rate and version) and a few Sensor,
Composite, Derived and Dial keys, and confirm they update.

## Mock bridge

```powershell
.\build\mock-bridge.exe                                   # gRPC + HTTP control on :9999
$env:MOCK_CONTROL_PORT = "12345"; .\build\mock-bridge.exe # custom port
Invoke-RestMethod http://localhost:9999/list                                        # current block + state
Invoke-RestMethod -Method Post -Uri http://localhost:9999/set  -Body '{"id":"TCPU","value":77}'
Invoke-RestMethod -Method Post -Uri http://localhost:9999/idle -Body '{"on":true}'  # freeze values (staleness)
Invoke-RestMethod -Method Post -Uri http://localhost:9999/down -Body '{"on":true}'  # simulate AIDA64 closed
Invoke-RestMethod -Method Post -Uri http://localhost:9999/reset
```
