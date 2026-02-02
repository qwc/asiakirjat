(function() {
    "use strict";

    var overlay = document.getElementById("asiakirjat-overlay");
    if (!overlay) return;

    // Push document content down so it's not hidden behind the fixed top bar
    var overlayHeight = overlay.offsetHeight;
    document.body.style.marginTop = overlayHeight + "px";

    var versionSelect = document.getElementById("asiakirjat-version-select");
    if (!versionSelect) return;

    var slug = versionSelect.getAttribute("data-slug");
    var current = versionSelect.getAttribute("data-current");

    // Fetch versions from API
    fetch("/api/project/" + encodeURIComponent(slug) + "/versions")
        .then(function(resp) { return resp.json(); })
        .then(function(versions) {
            // Clear and rebuild options
            versionSelect.innerHTML = "";
            versions.forEach(function(v) {
                var opt = document.createElement("option");
                opt.value = v.tag;
                opt.textContent = v.tag;
                if (v.tag === current) {
                    opt.selected = true;
                }
                versionSelect.appendChild(opt);
            });
        })
        .catch(function(err) {
            console.error("Failed to load versions:", err);
        });

    // Handle version switch
    versionSelect.addEventListener("change", function() {
        var newVersion = versionSelect.value;
        if (newVersion === current) return;

        // Preserve the current path within the doc
        var path = window.location.pathname;
        var prefix = "/project/" + slug + "/" + current;
        var suffix = path.substring(prefix.length);

        window.location.href = "/project/" + slug + "/" + newVersion + suffix;
    });

    // Overlay in-doc search
    var searchInput = document.getElementById("asiakirjat-overlay-search");
    var searchDropdown = document.getElementById("asiakirjat-overlay-search-dropdown");

    if (searchInput && searchDropdown) {
        var searchTimer = null;
        var searchSlug = searchInput.getAttribute("data-slug");
        var searchVersion = searchInput.getAttribute("data-version");

        function overlaySearch() {
            var q = searchInput.value.trim();
            if (q.length < 2) {
                searchDropdown.style.display = "none";
                searchDropdown.innerHTML = "";
                return;
            }

            var url = "/api/search?q=" + encodeURIComponent(q) +
                "&project=" + encodeURIComponent(searchSlug) +
                "&version=" + encodeURIComponent(searchVersion) +
                "&limit=8";

            fetch(url)
                .then(function(resp) { return resp.json(); })
                .then(function(data) {
                    searchDropdown.innerHTML = "";

                    if (!data.results || data.results.length === 0) {
                        var empty = document.createElement("div");
                        empty.className = "ao-search-empty";
                        empty.textContent = "No results found";
                        searchDropdown.appendChild(empty);
                        searchDropdown.style.display = "block";
                        return;
                    }

                    data.results.forEach(function(r) {
                        var item = document.createElement("a");
                        item.href = r.url;

                        var title = document.createElement("div");
                        title.className = "ao-search-item-title";
                        title.textContent = r.page_title || r.file_path;
                        item.appendChild(title);

                        if (r.snippet) {
                            var snippet = document.createElement("div");
                            snippet.className = "ao-search-item-snippet";
                            snippet.innerHTML = r.snippet;
                            item.appendChild(snippet);
                        }

                        searchDropdown.appendChild(item);
                    });

                    searchDropdown.style.display = "block";
                })
                .catch(function() {
                    searchDropdown.style.display = "none";
                });
        }

        searchInput.addEventListener("input", function() {
            clearTimeout(searchTimer);
            searchTimer = setTimeout(overlaySearch, 300);
        });

        searchInput.addEventListener("keydown", function(e) {
            if (e.key === "Escape") {
                searchDropdown.style.display = "none";
            }
        });

        document.addEventListener("click", function(e) {
            if (!searchInput.contains(e.target) && !searchDropdown.contains(e.target)) {
                searchDropdown.style.display = "none";
            }
        });
    }
})();
