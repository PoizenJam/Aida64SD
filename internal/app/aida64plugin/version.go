package aida64plugin

// hostVersion is this build's version. It must equal the manifest Version and
// PI_VERSION in pi_utils.js (pi_test/version_sync_test.js checks all three).
// It is compiled in rather than read from manifest.json because DRM-packed
// plugins treat the manifest as a protected asset that the plugin must not
// read at runtime. The PIs show it next to their own PI_VERSION so a stale
// install or cached PI scripts are visible.
const hostVersion = "1.0.0.0"
