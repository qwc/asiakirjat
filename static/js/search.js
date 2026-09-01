// Frontpage filtering.
//
// Two independent filters, applied together: free text over a project's name,
// slug and description, and an organization. Both are client-side — the page
// already holds every project the viewer may see, so filtering is a matter of
// hiding rather than fetching.
//
// Note this is a FILTER, not the search: full-text search across documentation
// content lives in the navbar and at /search. The placeholder says so, because
// two boxes on one page that do different things is exactly the sort of thing
// people click the wrong one of.
(function () {
    "use strict";

    var textInput = document.getElementById("search-input");
    var orgInput = document.getElementById("org-filter");
    var sections = Array.prototype.slice.call(document.querySelectorAll(".org-section"));
    var cards = Array.prototype.slice.call(document.querySelectorAll(".project-card"));

    if (!textInput && !orgInput) return;

    function matchesText(card, query) {
        if (!query) return true;
        var name = card.getAttribute("data-name") || "";
        var slug = card.getAttribute("data-slug") || "";
        var descEl = card.querySelector(".project-card-desc");
        var desc = descEl ? descEl.textContent.toLowerCase() : "";
        return name.indexOf(query) !== -1 || slug.indexOf(query) !== -1 || desc.indexOf(query) !== -1;
    }

    function apply() {
        var query = textInput ? textInput.value.toLowerCase().trim() : "";
        var org = orgInput ? orgInput.value.toLowerCase().trim() : "";

        sections.forEach(function (section) {
            var sectionOrg = section.getAttribute("data-org") || "";
            // Substring rather than equality: the org box is editable, so a
            // half-typed name should narrow rather than match nothing.
            var orgMatches = !org || sectionOrg.indexOf(org) !== -1;
            var visibleInSection = 0;

            Array.prototype.forEach.call(section.querySelectorAll(".project-card"), function (card) {
                var show = orgMatches && matchesText(card, query);
                card.classList.toggle("hidden", !show);
                if (show) visibleInSection++;
            });

            // A heading over nothing reads as an empty organization rather
            // than one filtered out, so the whole section goes.
            section.classList.toggle("hidden", visibleInSection === 0);
        });

        // Ungrouped pages have cards outside any section.
        if (sections.length === 0) {
            cards.forEach(function (card) {
                card.classList.toggle("hidden", !matchesText(card, query));
            });
        }
    }

    if (textInput) textInput.addEventListener("input", apply);
    if (orgInput) {
        orgInput.addEventListener("input", apply);
        orgInput.addEventListener("change", apply);
    }

    // Clicking an organization's heading filters to it, and clicking the one
    // already filtered to clears it — so the heading is a toggle rather than a
    // one-way trip that needs the input to undo.
    Array.prototype.forEach.call(document.querySelectorAll(".org-name"), function (button) {
        button.addEventListener("click", function () {
            if (!orgInput) return;
            var name = button.getAttribute("data-org-name") || "";
            orgInput.value = orgInput.value === name ? "" : name;
            apply();
        });
    });
})();
