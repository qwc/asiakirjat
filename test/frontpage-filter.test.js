// Browserless tests for the frontpage filters (static/js/search.js).
//
// They load the *real* search.js into a jsdom window over markup mirroring
// internal/templates/pages/frontpage.html, and drive the two filters. What
// they guard:
//
//   1. The two filters compose — narrowing by organization and by text at the
//      same time must intersect, not replace one another.
//   2. A section whose projects are all filtered out is hidden entirely. A
//      heading left standing over nothing reads as an empty organization
//      rather than one that was filtered away. This is asserted against the
//      REAL stylesheet, not the class list: the "hidden" class used to be
//      styled only for .project-card, so toggling it on a section did nothing
//      and a class-only assertion passed while the page was visibly wrong.
//   3. Clicking an organization heading toggles: clicking the one already
//      filtered to clears it, so the heading is not a one-way trip that needs
//      the input to undo.

const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const SEARCH_JS = fs.readFileSync(
  path.join(__dirname, "..", "static", "js", "search.js"),
  "utf8"
);

const STYLE_CSS = fs.readFileSync(
  path.join(__dirname, "..", "static", "css", "style.css"),
  "utf8"
);

// Mirrors the frontpage's grouped markup: two organizations, two projects each.
function markup() {
  const card = (name, slug, desc) => `
    <div class="project-card" data-name="${name.toLowerCase()}" data-slug="${slug}">
      <h3 class="project-card-title">${name}</h3>
      <p class="project-card-desc">${desc}</p>
    </div>`;

  return `<!DOCTYPE html><html><head><style>${STYLE_CSS}</style></head><body>
    <input type="text" id="org-filter">
    <input type="text" id="search-input">
    <section class="org-section" data-org="no org">
      <div class="org-heading">
        <button class="org-name" data-org-name="No Org">No Org</button>
      </div>
      <div class="project-grid">
        ${card("Handbook", "handbook", "company handbook")}
        ${card("Runbook", "runbook", "operations")}
      </div>
    </section>
    <section class="org-section" data-org="platform team">
      <div class="org-heading">
        <button class="org-name" data-org-name="Platform Team">Platform Team</button>
      </div>
      <div class="project-grid">
        ${card("Runtime", "runtime", "the runtime")}
      </div>
    </section>
  </body></html>`;
}

function setup() {
  const dom = new JSDOM(markup(), { runScripts: "outside-only" });
  dom.window.eval(SEARCH_JS);
  const doc = dom.window.document;

  const type = (id, value) => {
    const el = doc.getElementById(id);
    el.value = value;
    el.dispatchEvent(new dom.window.Event("input"));
  };

  const visibleCards = () =>
    Array.from(doc.querySelectorAll(".project-card"))
      .filter((c) => !c.classList.contains("hidden"))
      .map((c) => c.querySelector(".project-card-title").textContent);

  const visibleSections = () =>
    Array.from(doc.querySelectorAll(".org-section"))
      .filter((s) => !s.classList.contains("hidden"))
      .map((s) => s.getAttribute("data-org"));

  // Whether the browser would actually paint it, rather than whether the
  // class happens to be set.
  const isRendered = (el) =>
    dom.window.getComputedStyle(el).display !== "none";

  return { dom, doc, type, visibleCards, visibleSections, isRendered };
}

test("text filter matches name, slug and description", () => {
  const { type, visibleCards } = setup();

  type("search-input", "runbook");
  assert.deepStrictEqual(visibleCards(), ["Runbook"]);

  type("search-input", "operations"); // description only
  assert.deepStrictEqual(visibleCards(), ["Runbook"]);

  type("search-input", "");
  assert.deepStrictEqual(visibleCards(), ["Handbook", "Runbook", "Runtime"]);
});

test("organization filter narrows to one section", () => {
  const { type, visibleCards, visibleSections } = setup();

  type("org-filter", "Platform Team");
  assert.deepStrictEqual(visibleCards(), ["Runtime"]);
  assert.deepStrictEqual(visibleSections(), ["platform team"]);
});

test("a partially typed organization still narrows", () => {
  const { type, visibleSections } = setup();

  // The box is editable, so a half-typed name must narrow rather than match
  // nothing and blank the page.
  type("org-filter", "plat");
  assert.deepStrictEqual(visibleSections(), ["platform team"]);
});

test("the two filters intersect rather than replace each other", () => {
  const { type, visibleCards } = setup();

  type("search-input", "run"); // Runbook (No Org) and Runtime (Platform)
  assert.deepStrictEqual(visibleCards(), ["Runbook", "Runtime"]);

  type("org-filter", "No Org");
  assert.deepStrictEqual(visibleCards(), ["Runbook"]);
});

test("a section with nothing left is hidden, heading and all", () => {
  const { doc, type, visibleSections, isRendered } = setup();

  type("search-input", "runtime");
  assert.deepStrictEqual(
    visibleSections(),
    ["platform team"],
    "an empty organization heading must not be left standing"
  );

  // And it is really not painted — the class alone is not the point.
  const emptied = doc.querySelector('.org-section[data-org="no org"]');
  assert.ok(
    !isRendered(emptied),
    "the filtered-out section must actually be hidden by the stylesheet"
  );
  assert.ok(
    isRendered(doc.querySelector('.org-section[data-org="platform team"]')),
    "the matching section must still be visible"
  );
});

test("clicking an organization heading filters, and clicking it again clears", () => {
  const { doc, visibleCards } = setup();
  const button = doc.querySelector('[data-org-name="Platform Team"]');

  button.click();
  assert.strictEqual(doc.getElementById("org-filter").value, "Platform Team");
  assert.deepStrictEqual(visibleCards(), ["Runtime"]);

  button.click();
  assert.strictEqual(doc.getElementById("org-filter").value, "");
  assert.deepStrictEqual(visibleCards(), ["Handbook", "Runbook", "Runtime"]);
});

