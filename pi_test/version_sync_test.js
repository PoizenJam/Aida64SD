// Fails when manifest.json Version, the Go hostVersion constant, pi_utils.js
// PI_VERSION and the ?v= cache busters on the PI script tags disagree. A mismatch means a release would show
// "mismatch" in the Connection block, or Stream Deck could serve cached scripts.
// Usage: node version_sync_test.js <path to com.poizenjam.aida64sd.sdPlugin>
const fs = require("fs");
const path = require("path");
const dir = process.argv[2];
const manifest = JSON.parse(fs.readFileSync(path.join(dir, "manifest.json"), "utf8")).Version;
const m = /var PI_VERSION = "([^"]+)"/.exec(fs.readFileSync(path.join(dir, "pi_utils.js"), "utf8"));
let failed = 0;
function check(cond, msg) { console.log((cond ? "ok  : " : "FAIL: ") + msg); if (!cond) failed++; }
check(m && m[1] === manifest, "PI_VERSION " + (m && m[1]) + " == manifest " + manifest);
const goSrc = fs.readFileSync(path.join(dir, "..", "internal", "app", "aida64plugin", "version.go"), "utf8");
const g = /const hostVersion = "([^"]+)"/.exec(goSrc);
check(g && g[1] === manifest, "Go hostVersion " + (g && g[1]) + " == manifest " + manifest);
for (const f of fs.readdirSync(dir).filter((n) => n.endsWith("_pi.html"))) {
  const tags = fs.readFileSync(path.join(dir, f), "utf8").match(/<script src="[^"]+\.js\?v=[^"]+">/g) || [];
  for (const t of tags) check(t.indexOf("?v=" + manifest + '"') >= 0, f + ": " + t);
}
process.exitCode = failed ? 1 : 0;
