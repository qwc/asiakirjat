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
      <label id="asiakirjat-version-filter">
        <input type="checkbox" id="asiakirjat-show-all-versions">
      </label>
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

function versionsResponse(list = VERSIONS) {
  return Promise.resolve({ ok: true, json: () => Promise.resolve(list) });
}

// A project that publishes releases, release candidates and branch builds —
// the situation the version filter exists for (issue #128).
const MIXED_VERSIONS = [
  { tag: "v2.0.0", content_type: "archive" },
  { tag: "v2.0.0-rc1", content_type: "archive" },
  { tag: "v1.9", content_type: "archive" },
  { tag: "main", content_type: "archive" },
  { tag: "feature-login", content_type: "archive" },
];

function tagsIn(select) {
  return Array.from(select.options)
    .map((o) => o.value)
    .filter((v) => v !== "");
}

async function bootWithVersions({ dataCurrent, list }) {
  return boot({
    url: `http://localhost/project/docs/${dataCurrent}/guide.html`,
    dataCurrent,
    fetchImpl: (u) => {
      if (String(u).indexOf("/api/project/") !== -1) return versionsResponse(list);
      return Promise.resolve({ ok: false, text: () => Promise.resolve("") });
    },
  });
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

test("version picker hides prereleases and branch builds by default", async () => {
  const window = await bootWithVersions({ dataCurrent: "v2.0.0", list: MIXED_VERSIONS });
  const versionSelect = window.document.getElementById("asiakirjat-version-select");

  assert.deepStrictEqual(
    tagsIn(versionSelect),
    ["v2.0.0", "v1.9"],
    "only stable releases should be listed until 'All releases' is ticked"
  );
});

test("ticking 'All releases' reveals prereleases and branch builds", async () => {
  const window = await bootWithVersions({ dataCurrent: "v2.0.0", list: MIXED_VERSIONS });
  const versionSelect = window.document.getElementById("asiakirjat-version-select");
  const toggle = window.document.getElementById("asiakirjat-show-all-versions");

  toggle.checked = true;
  toggle.dispatchEvent(new window.Event("change"));
  await flush(window);

  assert.deepStrictEqual(
    tagsIn(versionSelect),
    MIXED_VERSIONS.map((v) => v.tag),
    "every version should be listed once the filter is off"
  );

  // And the choice is remembered for the next page.
  assert.strictEqual(window.localStorage.getItem("asiakirjat:show-all-versions"), "1");
});

test("the version being viewed is listed even when it is a prerelease", async () => {
  const window = await bootWithVersions({ dataCurrent: "v2.0.0-rc1", list: MIXED_VERSIONS });
  const versionSelect = window.document.getElementById("asiakirjat-version-select");

  assert.ok(
    tagsIn(versionSelect).includes("v2.0.0-rc1"),
    "the picker must be able to show the version currently being read"
  );
  assert.strictEqual(
    versionSelect.value,
    "v2.0.0-rc1",
    "and it must be the selected option"
  );
  assert.ok(
    !tagsIn(versionSelect).includes("main"),
    "other unstable versions stay hidden"
  );
});

test("the filter is hidden when there is nothing to filter", async () => {
  const window = await bootWithVersions({
    dataCurrent: "main",
    list: [
      { tag: "main", content_type: "archive" },
      { tag: "feature-login", content_type: "archive" },
    ],
  });
  const versionSelect = window.document.getElementById("asiakirjat-version-select");
  const wrap = window.document.getElementById("asiakirjat-version-filter");

  assert.strictEqual(wrap.style.display, "none", "no stable releases: the checkbox is pointless");
  assert.deepStrictEqual(
    tagsIn(versionSelect),
    ["main", "feature-login"],
    "and every version stays listed rather than the picker going empty"
  );
});

test("the compare dropdown follows the same filter", async () => {
  const window = await bootWithVersions({ dataCurrent: "v2.0.0", list: MIXED_VERSIONS });
  const compare = window.document.getElementById("asiakirjat-compare-select");

  assert.deepStrictEqual(
    tagsIn(compare),
    ["v1.9"],
    "compare lists stable releases other than the current one"
  );

  const toggle = window.document.getElementById("asiakirjat-show-all-versions");
  toggle.checked = true;
  toggle.dispatchEvent(new window.Event("change"));
  await flush(window);

  assert.deepStrictEqual(
    tagsIn(compare),
    ["v2.0.0-rc1", "v1.9", "main", "feature-login"],
    "and everything but the current version once the filter is off"
  );
});
