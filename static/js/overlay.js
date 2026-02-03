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
                        item.href = r.url + "?highlight=" + encodeURIComponent(q);

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

    // Highlight search terms from URL parameter
    function highlightSearchTerms() {
        var params = new URLSearchParams(window.location.search);
        var highlight = params.get("highlight");
        if (!highlight) return;

        var terms = highlight.toLowerCase().split(/\s+/).filter(function(t) {
            return t.length >= 2;
        });
        if (terms.length === 0) return;

        // Find the main content area (iframe or document body)
        var content = document.body;

        // Walk text nodes and wrap matches
        var walker = document.createTreeWalker(
            content,
            NodeFilter.SHOW_TEXT,
            {
                acceptNode: function(node) {
                    // Skip script, style, and already marked elements
                    var parent = node.parentNode;
                    if (!parent) return NodeFilter.FILTER_REJECT;
                    var tag = parent.tagName;
                    if (tag === "SCRIPT" || tag === "STYLE" || tag === "MARK" ||
                        tag === "NOSCRIPT" || tag === "TEXTAREA" || tag === "INPUT") {
                        return NodeFilter.FILTER_REJECT;
                    }
                    // Skip overlay elements
                    if (parent.closest && parent.closest("#asiakirjat-overlay")) {
                        return NodeFilter.FILTER_REJECT;
                    }
                    return NodeFilter.FILTER_ACCEPT;
                }
            }
        );

        var nodesToProcess = [];
        while (walker.nextNode()) {
            nodesToProcess.push(walker.currentNode);
        }

        nodesToProcess.forEach(function(textNode) {
            var text = textNode.textContent;
            var lowerText = text.toLowerCase();
            var hasMatch = terms.some(function(term) {
                return lowerText.indexOf(term) !== -1;
            });
            if (!hasMatch) return;

            // Build regex to match any term
            var escapedTerms = terms.map(function(t) {
                return t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
            });
            var regex = new RegExp("(" + escapedTerms.join("|") + ")", "gi");
            var parts = text.split(regex);

            if (parts.length <= 1) return;

            var frag = document.createDocumentFragment();
            parts.forEach(function(part) {
                if (regex.test(part)) {
                    var mark = document.createElement("mark");
                    mark.className = "search-highlight";
                    mark.textContent = part;
                    frag.appendChild(mark);
                    regex.lastIndex = 0; // reset regex
                } else {
                    frag.appendChild(document.createTextNode(part));
                }
            });

            textNode.parentNode.replaceChild(frag, textNode);
        });

        // Scroll to first highlight
        var firstMark = document.querySelector("mark.search-highlight");
        if (firstMark) {
            firstMark.scrollIntoView({ behavior: "smooth", block: "center" });
        }
    }

    // Run highlighting after DOM is ready
    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", highlightSearchTerms);
    } else {
        highlightSearchTerms();
    }
})();
