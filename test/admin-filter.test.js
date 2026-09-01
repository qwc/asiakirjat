// Browserless tests for the admin list filter (static/js/admin-filter.js).
//
// The script is shared by every admin page that lists cards, so what matters
// is that it stays driven by markup rather than by knowledge of any one page:
// an input names the items it filters, an item carries the text to match. Add
// a third list and no JavaScript should need touching.
//
// As with the frontpage filter, hiding is asserted against the real stylesheet
// rather than the class list — a "hidden" class nothing styles looks fine to a
// class-based assertion and wrong to a person.

const { test } = require("node:test");
const assert = require("node:assert");
const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const ADMIN_FILTER_JS = fs.readFileSync(
  path.join(__dirname, "..", "static", "js", "admin-filter.js"),
  "utf8"
);

const STYLE_CSS = fs.readFileSync(
  path.join(__dirname, "..", "static", "css", "style.css"),
  "utf8"
);

// Mirrors internal/templates/pages/admin_orgs.html.
function markup() {
  const card = (name, text) => `
    <div class="access-card" data-filter-text="${text}">
      <h2>${name}</h2>
    </div>`;

  return `<!DOCTYPE html><html><head><style>${STYLE_CSS}</style></head><body>
    <input type="text" class="admin-filter" data-filter-items=".access-card">
    ${card("No Org", "no org default projects that predate organizations")}
    ${card("Platform Team", "platform team platform the platform group")}
    ${card("Writers", "writers docs team")}
  </body></html>`;
}

function setup() {
  const dom = new JSDOM(markup(), { runScripts: "outside-only" });
  dom.window.eval(ADMIN_FILTER_JS);
  const doc = dom.window.document;

  const type = (value) => {
    const el = doc.querySelector(".admin-filter");
    el.value = value;
    el.dispatchEvent(new dom.window.Event("input"));
  };

  const rendered = () =>
    Array.from(doc.querySelectorAll(".access-card"))
      .filter((c) => dom.window.getComputedStyle(c).display !== "none")
      .map((c) => c.querySelector("h2").textContent);

  return { doc, type, rendered };
}

test("filtering narrows the list to matching cards", () => {
  const { type, rendered } = setup();

  type("platform");
  assert.deepStrictEqual(rendered(), ["Platform Team"]);
});

test("it matches the card's filter text, not only its heading", () => {
  const { type, rendered } = setup();

  // "docs" appears only in the Writers card's filter text (its description),
  // which is the point of carrying that attribute at all.
  type("docs");
  assert.deepStrictEqual(rendered(), ["Writers"]);
});

test("clearing the box brings everything back", () => {
  const { type, rendered } = setup();

  type("platform");
  assert.strictEqual(rendered().length, 1);

  type("");
  assert.deepStrictEqual(rendered(), ["No Org", "Platform Team", "Writers"]);
});

test("a query matching nothing hides everything without erroring", () => {
  const { type, rendered } = setup();

  type("zzzzz");
  assert.deepStrictEqual(rendered(), []);
});

test("a page with no filter input is left alone", () => {
  const dom = new JSDOM(
    `<!DOCTYPE html><html><body><div class="access-card">Only</div></body></html>`,
    { runScripts: "outside-only" }
  );
  // The script must not assume its input exists: every admin page loads it,
  // and the ones with a single card render no filter at all.
  assert.doesNotThrow(() => dom.window.eval(ADMIN_FILTER_JS));
});
