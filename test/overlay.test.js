// Browserless E2E-style tests for the documentation overlay (static/js/overlay.js).
//
// They load the *real* overlay.js into a jsdom window, mock the versions API
// and the document fetch, and drive the compare flow — exercising the code in
// a DOM without needing a real browser. These guard the bugs that broke the
// diff view on the rolling /latest/ permalink:
//
//   1. Path math must strip the version segment actually in the URL
//      ("latest"), not the resolved tag (data-current, e.g. v1.5), or the
//      compare fetch builds a mangled URL.
//   2. showError must position the diff-indicator bar below the overlay
//      (set style.top), or it renders behind the overlay as an empty bar.

const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const OVERLAY_JS = fs.readFileSync(
  path.join(__dirname, "..", "static", "js", "overlay.js"),
  "utf8"
);

const VERSIONS = [
  { tag: "v1.5", content_type: "archive" },
  { tag: "v1.0.0", content_type: "archive" },
];

// Minimal markup mirroring internal/templates/overlay/doc_overlay.html — only
// the elements overlay.js looks up by id, plus a content container that
// findContentContainer() matches ([role="main"]).
function markup(dataCurrent) {
  return `<!DOCTYPE html><html><body>
    <div id="asiakirjat-overlay">
      <select id="asiakirjat-version-select" data-slug="docs" data-current="${dataCurrent}"></select>
      <select id="asiakirjat-compare-select" data-slug="docs" data-current="${dataCurrent}">
        <option value="">Select version...</option>
      </select>
      <a id="asiakirjat-download-link"></a>
    </div>
    <div id="asiakirjat-diff-indicator" style="display:none;">
      <span>Showing changes from version <strong id="asiakirjat-diff-from-version"></strong>
        — <strong id="asiakirjat-diff-change-info" style="display:none;"></strong></span>
      <span id="asiakirjat-diff-nav" style="display:none;">
        <button id="asiakirjat-prev-change"></button>
        <span id="asiakirjat-diff-change-counter"></span>
        <button id="asiakirjat-next-change"></button>
      </span>
      <button id="asiakirjat-exit-diff">Exit Diff View</button>
    </div>
    <div role="main"><p>original content</p></div>
  </body></html>`;
}

// Resolve microtasks/timers so the versions fetch can populate the dropdowns.
function flush(window, ticks = 4) {
  return new Promise((resolve) => {
    let i = 0;
    (function tick() {
      if (i++ >= ticks) return resolve();
      window.setTimeout(tick, 0);
    })();
  });
}

// Boot a jsdom window with overlay.js loaded. fetchImpl(url) returns the fetch
// result. Returns the window once the initial version fetches have settled.
async function boot({ url, dataCurrent, fetchImpl }) {
  const dom = new JSDOM(markup(dataCurrent), { runScripts: "dangerously", url });
  const { window } = dom;
  window.BASE_PATH = "";
  if (!window.CSS) window.CSS = {};
  if (typeof window.CSS.escape !== "function") window.CSS.escape = (s) => String(s);
  window.fetch = fetchImpl;

  const script = window.document.createElement("script");
  script.textContent = OVERLAY_JS;
  window.document.body.appendChild(script);

  await flush(window);
  return window;
}

function versionsResponse() {
  return Promise.resolve({ ok: true, json: () => Promise.resolve(VERSIONS) });
}

// Drive a compare against `targetVersion`. The document fetch is forced to fail
// (ok:false) so we land on the error path without needing htmldiff/DOMParser —
// and we capture the URL the overlay tried to fetch.
async function compareCapturingUrl({ url, dataCurrent, targetVersion }) {
  let fetchedDocUrl = null;
  const window = await boot({
    url,
    dataCurrent,
    fetchImpl: (u) => {
      if (String(u).indexOf("/api/project/") !== -1) return versionsResponse();
      fetchedDocUrl = String(u);
      return Promise.resolve({ ok: false, text: () => Promise.resolve("") });
    },
  });

  const compare = window.document.getElementById("asiakirjat-compare-select");
  compare.value = targetVersion;
  compare.dispatchEvent(new window.Event("change"));
  await flush(window);

  return { window, fetchedDocUrl };
}

test("compare from a /latest/ URL fetches the resolved version path, not a mangled one", async () => {
  const { fetchedDocUrl } = await compareCapturingUrl({
    url: "http://localhost/project/docs/latest/guide.html",
    dataCurrent: "v1.5", // server resolved "latest" -> v1.5
    targetVersion: "v1.0.0",
  });
  assert.strictEqual(
    fetchedDocUrl,
    "/project/docs/v1.0.0/guide.html",
    "must strip the URL 'latest' segment, not the resolved tag (regression: produced .../v1.0.0st/guide.html)"
  );
});

test("compare from an explicit version URL still builds the correct path", async () => {
  const { fetchedDocUrl } = await compareCapturingUrl({
    url: "http://localhost/project/docs/v1.0.0/guide.html",
    dataCurrent: "v1.0.0",
    targetVersion: "v1.5",
  });
  assert.strictEqual(fetchedDocUrl, "/project/docs/v1.5/guide.html");
});

test("error bar is shown positioned below the overlay (not hidden behind it)", async () => {
  const { window } = await compareCapturingUrl({
    url: "http://localhost/project/docs/latest/guide.html",
    dataCurrent: "v1.5",
    targetVersion: "v1.0.0",
  });
  const indicator = window.document.getElementById("asiakirjat-diff-indicator");
  assert.strictEqual(indicator.style.display, "flex", "indicator must be shown");
  assert.notStrictEqual(
    indicator.style.top,
    "",
    "showError must set style.top so the bar isn't rendered behind the overlay"
  );
  // The static header text and Exit button remain in the bar.
  assert.match(indicator.textContent, /Showing changes from version/);
  assert.ok(window.document.getElementById("asiakirjat-exit-diff"));
});
