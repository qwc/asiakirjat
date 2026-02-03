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

    // Version comparison feature
    var compareSelect = document.getElementById("asiakirjat-compare-select");
    var diffModal = document.getElementById("asiakirjat-diff-modal");
    var diffBody = document.getElementById("asiakirjat-diff-body");
    var diffTitle = document.getElementById("asiakirjat-diff-title");
    var diffClose = document.getElementById("asiakirjat-diff-close");

    if (compareSelect && diffModal) {
        // Populate compare dropdown with versions (excluding current)
        fetch("/api/project/" + encodeURIComponent(slug) + "/versions")
            .then(function(resp) { return resp.json(); })
            .then(function(versions) {
                compareSelect.innerHTML = '<option value="">Select version...</option>';
                versions.forEach(function(v) {
                    if (v.tag !== current) {
                        var opt = document.createElement("option");
                        opt.value = v.tag;
                        opt.textContent = v.tag;
                        compareSelect.appendChild(opt);
                    }
                });
            })
            .catch(function(err) {
                console.error("Failed to load versions for compare:", err);
            });

        // Simple line-based diff algorithm
        function computeDiff(oldText, newText) {
            var oldLines = oldText.split("\n");
            var newLines = newText.split("\n");
            var result = [];

            // Simple LCS-based diff
            var dp = [];
            for (var i = 0; i <= oldLines.length; i++) {
                dp[i] = [];
                for (var j = 0; j <= newLines.length; j++) {
                    if (i === 0 || j === 0) {
                        dp[i][j] = 0;
                    } else if (oldLines[i - 1] === newLines[j - 1]) {
                        dp[i][j] = dp[i - 1][j - 1] + 1;
                    } else {
                        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
                    }
                }
            }

            // Backtrack to find diff
            var i = oldLines.length, j = newLines.length;
            var stack = [];
            while (i > 0 || j > 0) {
                if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
                    stack.push({ type: "same", text: oldLines[i - 1] });
                    i--; j--;
                } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
                    stack.push({ type: "add", text: newLines[j - 1] });
                    j--;
                } else {
                    stack.push({ type: "del", text: oldLines[i - 1] });
                    i--;
                }
            }

            // Reverse to get correct order
            while (stack.length > 0) {
                result.push(stack.pop());
            }
            return result;
        }

        function escapeHtml(text) {
            var div = document.createElement("div");
            div.appendChild(document.createTextNode(text));
            return div.innerHTML;
        }

        function renderDiff(diff) {
            var html = [];
            diff.forEach(function(line) {
                var escaped = escapeHtml(line.text);
                if (line.type === "add") {
                    html.push('<span class="diff-add">+ ' + escaped + '</span>');
                } else if (line.type === "del") {
                    html.push('<span class="diff-del">- ' + escaped + '</span>');
                } else {
                    html.push('  ' + escaped);
                }
            });
            return html.join("\n");
        }

        function extractTextContent(html) {
            var parser = new DOMParser();
            var doc = parser.parseFromString(html, "text/html");
            // Remove script and style elements
            var scripts = doc.querySelectorAll("script, style, noscript");
            scripts.forEach(function(el) { el.remove(); });
            return doc.body.textContent || "";
        }

        compareSelect.addEventListener("change", function() {
            var targetVersion = compareSelect.value;
            if (!targetVersion) return;

            // Get current document path
            var path = window.location.pathname;
            var prefix = "/project/" + slug + "/" + current;
            var suffix = path.substring(prefix.length);

            // Build URLs for both versions
            var currentUrl = "/project/" + slug + "/" + current + suffix;
            var targetUrl = "/project/" + slug + "/" + targetVersion + suffix;

            diffTitle.textContent = "Comparing " + current + " → " + targetVersion;
            diffBody.innerHTML = '<div id="asiakirjat-diff-loading">Loading...</div>';
            diffModal.classList.add("ao-visible");

            // Fetch both versions
            Promise.all([
                fetch(currentUrl).then(function(r) { return r.text(); }),
                fetch(targetUrl).then(function(r) {
                    if (!r.ok) throw new Error("Page not found in " + targetVersion);
                    return r.text();
                })
            ])
            .then(function(results) {
                var currentText = extractTextContent(results[0]);
                var targetText = extractTextContent(results[1]);
                var diff = computeDiff(currentText, targetText);
                diffBody.innerHTML = renderDiff(diff);
            })
            .catch(function(err) {
                diffBody.innerHTML = '<div style="color: #dc2626; padding: 1rem;">Error: ' + escapeHtml(err.message) + '</div>';
            });

            // Reset select
            compareSelect.value = "";
        });

        // Close modal handlers
        diffClose.addEventListener("click", function() {
            diffModal.classList.remove("ao-visible");
        });

        diffModal.addEventListener("click", function(e) {
            if (e.target === diffModal) {
                diffModal.classList.remove("ao-visible");
            }
        });

        document.addEventListener("keydown", function(e) {
            if (e.key === "Escape" && diffModal.classList.contains("ao-visible")) {
                diffModal.classList.remove("ao-visible");
            }
        });
    }

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

                    // Add "View all results" link if there are more results
                    if (data.total > 8) {
                        var viewAll = document.createElement("a");
                        viewAll.className = "ao-search-view-all";
                        viewAll.href = "/search?q=" + encodeURIComponent(q) +
                            "&project=" + encodeURIComponent(searchSlug);
                        viewAll.textContent = "View all " + data.total + " results";
                        searchDropdown.appendChild(viewAll);
                    }

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
