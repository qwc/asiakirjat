(function() {
    "use strict";

    var input = document.getElementById("navbar-search-input");
    if (!input) return;

    var dropdown = document.getElementById("navbar-search-dropdown");
    if (!dropdown) return;

    var basePath = window.BASE_PATH || "";
    var timer = null;

    function debounce(fn, delay) {
        return function() {
            clearTimeout(timer);
            timer = setTimeout(fn, delay);
        };
    }

    function escapeHTML(str) {
        var div = document.createElement("div");
        div.appendChild(document.createTextNode(str));
        return div.innerHTML;
    }

    function doSearch() {
        var q = input.value.trim();
        if (q.length < 2) {
            dropdown.style.display = "none";
            dropdown.innerHTML = "";
            return;
        }

        fetch(basePath + "/api/search?q=" + encodeURIComponent(q) + "&limit=8")
            .then(function(resp) { return resp.json(); })
            .then(function(data) {
                dropdown.innerHTML = "";

                if (!data.results || data.results.length === 0) {
                    var empty = document.createElement("div");
                    empty.className = "navbar-search-empty";
                    empty.textContent = "No results found";
                    dropdown.appendChild(empty);
                    dropdown.style.display = "block";
                    return;
                }

                data.results.forEach(function(r) {
                    var item = document.createElement("a");
                    item.className = "navbar-search-item";
                    item.href = r.url + "?highlight=" + encodeURIComponent(q);

                    var title = document.createElement("div");
                    title.className = "navbar-search-item-title";
                    title.textContent = r.page_title || r.file_path;
                    item.appendChild(title);

                    var meta = document.createElement("div");
                    meta.className = "navbar-search-item-meta";
                    meta.textContent = r.project_name + " / " + r.version_tag;
                    item.appendChild(meta);

                    if (r.snippet) {
                        var snippet = document.createElement("div");
                        snippet.className = "navbar-search-item-snippet";
                        snippet.innerHTML = r.snippet;
                        item.appendChild(snippet);
                    }

                    dropdown.appendChild(item);
                });

                if (data.total > 8) {
                    var viewAll = document.createElement("a");
                    viewAll.className = "navbar-search-view-all";
                    viewAll.href = basePath + "/search?q=" + encodeURIComponent(q);
                    viewAll.textContent = "View all " + data.total + " results";
                    dropdown.appendChild(viewAll);
                }

                dropdown.style.display = "block";
            })
            .catch(function() {
                dropdown.style.display = "none";
            });
    }

    input.addEventListener("input", debounce(doSearch, 300));

    // Keyboard navigation
    var selectedIndex = -1;

    function getSelectableItems() {
        return dropdown.querySelectorAll("a.navbar-search-item, a.navbar-search-view-all");
    }

    function updateSelection() {
        var items = getSelectableItems();
        items.forEach(function(item, i) {
            if (i === selectedIndex) {
                item.classList.add("navbar-search-item-selected");
            } else {
                item.classList.remove("navbar-search-item-selected");
            }
        });
        // Scroll selected item into view
        if (selectedIndex >= 0 && items[selectedIndex]) {
            items[selectedIndex].scrollIntoView({ block: "nearest" });
        }
    }

    input.addEventListener("keydown", function(e) {
        var items = getSelectableItems();
        var visible = dropdown.style.display === "block";

        if (e.key === "Escape") {
            dropdown.style.display = "none";
            selectedIndex = -1;
            return;
        }

        if (!visible || items.length === 0) return;

        if (e.key === "ArrowDown") {
            e.preventDefault();
            selectedIndex = (selectedIndex + 1) % items.length;
            updateSelection();
        } else if (e.key === "ArrowUp") {
            e.preventDefault();
            selectedIndex = selectedIndex <= 0 ? items.length - 1 : selectedIndex - 1;
            updateSelection();
        } else if (e.key === "Enter") {
            if (selectedIndex >= 0 && items[selectedIndex]) {
                e.preventDefault();
                items[selectedIndex].click();
            }
        }
    });

    // Reset selection when search results change
    var originalDoSearch = doSearch;
    doSearch = function() {
        selectedIndex = -1;
        originalDoSearch();
    };

    // Close dropdown when clicking outside
    document.addEventListener("click", function(e) {
        if (!input.contains(e.target) && !dropdown.contains(e.target)) {
            dropdown.style.display = "none";
        }
    });

    // Show dropdown when input is focused with existing value
    input.addEventListener("focus", function() {
        if (input.value.trim().length >= 2 && dropdown.children.length > 0) {
            dropdown.style.display = "block";
        }
    });
})();
