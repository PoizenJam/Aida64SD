# Changelog

## 1.0.0 (first public release)

Aida64SD shows live AIDA64 sensor readings on an Elgato Stream Deck. It is a
modified version of [hwinfo-streamdeck](https://github.com/moeilijk/hwinfo-streamdeck)
(GPL-3.0) with an AIDA64 backend.

### Actions

- **Sensor**: one reading per key as a graph, text, or both. Pick an AIDA64
  sensor category, then a reading; per-tile colours, Min/Max scale or auto
  range, format string, divisor and graph unit; per-tile thresholds.
- **Composite**: up to four readings on one key, each with its own colour
  and graph, or as text only.
- **Derived**: a new metric from 2 to 8 readings (sum, average, max, min,
  delta, percent).
- **Dial Carousel**: cycle readings on a Stream Deck+ dial, with threshold
  indicators.
- **Settings**: a key showing the applied refresh interval, and a Property
  Inspector with the refresh rate, the AIDA64.INI options, the key's
  appearance, the global threshold library and the connection status.

### Features

- Reads AIDA64's shared memory (`AIDA64_SensorValues`) through a separate
  bridge process; works with AIDA64 Extreme, Engineer and Business.
- Auto refresh follows AIDA64's own sensor update rate from AIDA64.INI, with
  optional INI path and rate-key overrides; or pick a fixed 250 ms to 10 s.
- Thresholds recolour a tile and show alert text, with hysteresis, cooldown
  and snooze; a shared library of global thresholds applies by reading type,
  and each tile can opt out.
- While AIDA64 is closed, configured Sensor, Composite and Derived keys show
  "AIDA64 not detected" over dimmed key art (unconfigured keys keep the plain
  art), and they recover by themselves when it returns.
- Restarts the bridge if it crashes or stops responding.
- Windows build and install script (`build.ps1`) alongside the Linux/WSL
  Makefile.
