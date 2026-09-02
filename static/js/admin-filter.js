// Filtering for the admin list pages.
//
// One script for every page that shows a list of cards long enough to want
// narrowing — organizations, access groups. Each filter input names the items
// it acts on, and each item carries the text it should be matched against, so
// adding another list means adding markup rather than JavaScript:
//
//   <input class="admin-filter" data-filter-items=".access-card">
//   <div class="access-card" data-filter-text="platform team dev">…</div>
//
// Matching is a plain substring over that text. These lists are tens of items
// on the largest instances, so anything cleverer would be solving a problem
// nobody has.
(function () {
    "use strict";

    var inputs = document.querySelectorAll(".admin-filter");
    if (!inputs.length) return;

    Array.prototype.forEach.call(inputs, function (input) {
        var selector = input.getAttribute("data-filter-items");
        if (!selector) return;

        var items = Array.prototype.slice.call(document.querySelectorAll(selector));

        input.addEventListener("input", function () {
            var query = input.value.toLowerCase().trim();

            items.forEach(function (item) {
                var haystack = item.getAttribute("data-filter-text") || item.textContent.toLowerCase();
                item.classList.toggle("hidden", Boolean(query) && haystack.indexOf(query) === -1);
            });
        });
    });
})();
