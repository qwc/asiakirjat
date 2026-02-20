(function() {
    "use strict";

    var overlay = document.getElementById("asiakirjat-overlay");
    if (!overlay) return;

    var basePath = window.BASE_PATH || "";

    // Push document content down so it's not hidden behind the fixed top bar
    var overlayHeight = overlay.offsetHeight;
    document.body.style.marginTop = overlayHeight + "px";

    var versionSelect = document.getElementById("asiakirjat-version-select");
    if (!versionSelect) return;

    var slug = versionSelect.getAttribute("data-slug");
    var current = versionSelect.getAttribute("data-current");

    // Fetch versions from API
    fetch(basePath + "/api/project/" + encodeURIComponent(slug) + "/versions")
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
        var prefix = basePath + "/project/" + slug + "/" + current;
        var suffix = path.substring(prefix.length);

        window.location.href = basePath + "/project/" + slug + "/" + newVersion + suffix;
    });

    // Update download link when version changes
    var downloadLink = document.getElementById("asiakirjat-download-link");
    if (downloadLink) {
        downloadLink.href = basePath + "/project/" + slug + "/version/" + current + "/download";
    }

    // Version comparison feature - inline diff
    var compareSelect = document.getElementById("asiakirjat-compare-select");
    var diffIndicator = document.getElementById("asiakirjat-diff-indicator");
    var diffFromVersion = document.getElementById("asiakirjat-diff-from-version");
    var exitDiffBtn = document.getElementById("asiakirjat-exit-diff");

    // State for diff mode
    var originalContentHtml = null;
    var contentContainer = null;
    var diffModeActive = false;
    var diffChanges = [];
    var currentChangeIndex = -1;

    var currentIsPdf = false;

    if (compareSelect) {
        // Populate compare dropdown with versions (excluding current)
        fetch(basePath + "/api/project/" + encodeURIComponent(slug) + "/versions")
            .then(function(resp) { return resp.json(); })
            .then(function(versions) {
                compareSelect.innerHTML = '<option value="">Select version...</option>';
                versions.forEach(function(v) {
                    if (v.tag === current && v.content_type === "pdf") {
                        currentIsPdf = true;
                    }
                    if (v.tag !== current) {
                        var opt = document.createElement("option");
                        opt.value = v.tag;
                        opt.textContent = v.tag;
                        if (v.content_type) {
                            opt.setAttribute("data-content-type", v.content_type);
                        }
                        compareSelect.appendChild(opt);
                    }
                });

                // Auto-trigger diff if ?compare= is in URL
                var initParams = new URLSearchParams(window.location.search);
                var compareParam = initParams.get("compare");
                if (compareParam && compareSelect.querySelector('option[value="' + CSS.escape(compareParam) + '"]')) {
                    compareSelect.value = compareParam;
                    compareSelect.dispatchEvent(new Event("change"));
                }
            })
            .catch(function(err) {
                console.error("Failed to load versions for compare:", err);
            });

        // Find the main content container (works with various doc themes)
        function findContentContainer() {
            // Try known theme-specific selectors first (most specific)
            var themeSelectors = [
                // MkDocs Material
                '.md-content__inner > .md-typeset',
                '.md-content__inner',
                // MkDocs default
                '.content article',
                // Sphinx ReadTheDocs
                '.rst-content',
                'div[itemprop="articleBody"]',
                '.document .documentwrapper .bodywrapper .body',
                '.document .body',
                // Sphinx alabaster/basic
                'div.body',
                'div.document',
                // Doxygen
                '.contents',
                '.doc-content',
                '#doc-content',
                // Hugo docsy
                '.td-content',
                // Docusaurus
                '.markdown',
                'article .markdown',
                // GitBook
                '.page-inner .page-body',
                '.book-body .body-inner',
                // VuePress
                '.theme-default-content',
                // Generic semantic
                'main [role="main"]',
                'article[role="main"]',
                '[role="main"]'
            ];

            for (var i = 0; i < themeSelectors.length; i++) {
                var el = document.querySelector(themeSelectors[i]);
                if (el && !isOverlayElement(el)) return el;
            }

            // Try semantic HTML5 elements
            var main = document.querySelector('main');
            if (main && !isOverlayElement(main) && hasSubstantialContent(main)) {
                return main;
            }

            var article = document.querySelector('article');
            if (article && !isOverlayElement(article) && hasSubstantialContent(article)) {
                return article;
            }

            // Heuristic: find the element with the most text content
            // that doesn't look like navigation/sidebar
            var best = findLargestContentBlock();
            if (best) return best;

            return null;
        }

        function isOverlayElement(el) {
            return el.closest('#asiakirjat-overlay') || el.closest('#asiakirjat-diff-indicator');
        }

        function hasSubstantialContent(el) {
            var text = (el.textContent || '').trim();
            return text.length > 200; // At least some meaningful content
        }

        function findLargestContentBlock() {
            var candidates = document.querySelectorAll('div, section');
            var best = null;
            var bestScore = 0;

            // Patterns that indicate non-content areas
            var excludePattern = /nav|sidebar|menu|header|footer|toc|breadcrumb|pagination|search|overlay|modal|drawer/i;

            for (var i = 0; i < candidates.length; i++) {
                var el = candidates[i];

                // Skip overlay elements
                if (isOverlayElement(el)) continue;

                // Skip elements that look like navigation/sidebar
                var identifier = (el.className || '') + ' ' + (el.id || '');
                if (excludePattern.test(identifier)) continue;

                // Skip if it contains very little text
                var text = (el.textContent || '').trim();
                if (text.length < 500) continue;

                // Score: prefer elements with more paragraphs and less nesting of other large blocks
                var paragraphs = el.querySelectorAll('p, li, pre, code').length;
                var score = text.length + (paragraphs * 100);

                // Penalize if it's a wrapper of almost the entire body
                var bodyText = (document.body.textContent || '').length;
                if (text.length > bodyText * 0.9) {
                    score = score * 0.5; // Likely too broad
                }

                if (score > bestScore) {
                    bestScore = score;
                    best = el;
                }
            }

            return best;
        }

        // Build a list of selectors to try for an element
        function getSelectorsForElement(el) {
            var selectors = [];

            // ID selector (most reliable)
            if (el.id) {
                selectors.push('#' + el.id);
            }

            // Class-based selectors
            if (el.className && typeof el.className === 'string') {
                var classes = el.className.split(' ').filter(function(c) { return c.trim(); });
                if (classes.length > 0) {
                    // Try tag + all classes
                    selectors.push(el.tagName.toLowerCase() + '.' + classes.join('.'));
                    // Try just the first distinctive class
                    for (var i = 0; i < classes.length; i++) {
                        if (!/^(container|wrapper|row|col|clearfix)$/i.test(classes[i])) {
                            selectors.push('.' + classes[i]);
                            break;
                        }
                    }
                }
            }

            // Tag name as last resort
            selectors.push(el.tagName.toLowerCase());

            return selectors;
        }

        // Extract content from fetched HTML trying multiple selectors
        function extractContentFromHtml(html, selectors) {
            var parser = new DOMParser();
            var doc = parser.parseFromString(html, "text/html");

            // Try each selector
            for (var i = 0; i < selectors.length; i++) {
                var el = doc.querySelector(selectors[i]);
                if (el) {
                    return el.innerHTML;
                }
            }

            // Fallback: try to find content using same theme-specific selectors
            var themeSelectors = [
                '.md-content__inner > .md-typeset',
                '.md-content__inner',
                '.rst-content',
                'div[itemprop="articleBody"]',
                '.document .body',
                'div.body',
                '.contents',
                '.td-content',
                '.markdown',
                'main',
                'article'
            ];

            for (var i = 0; i < themeSelectors.length; i++) {
                var el = doc.querySelector(themeSelectors[i]);
                if (el) {
                    return el.innerHTML;
                }
            }

            return null;
        }

        // Collect change regions from diffed content
        function collectChanges(container) {
            var elements = container.querySelectorAll("ins, del");
            var groups = [];
            var visited = [];

            for (var i = 0; i < elements.length; i++) {
                if (visited.indexOf(elements[i]) !== -1) continue;

                var group = [elements[i]];
                visited.push(elements[i]);

                // Group adjacent ins/del elements (e.g. del immediately followed by ins = one change)
                var next = elements[i].nextElementSibling;
                while (next && (next.tagName === "INS" || next.tagName === "DEL") && visited.indexOf(next) === -1) {
                    group.push(next);
                    visited.push(next);
                    next = next.nextElementSibling;
                }

                groups.push(group);
            }

            return groups;
        }

        // Scroll to a specific change and highlight it
        function scrollToChange(index) {
            if (diffChanges.length === 0) return;

            // Remove highlight from previous
            var prev = document.querySelectorAll(".asiakirjat-current-change");
            for (var i = 0; i < prev.length; i++) {
                prev[i].classList.remove("asiakirjat-current-change");
            }

            // Add highlight to new
            var group = diffChanges[index];
            for (var i = 0; i < group.length; i++) {
                group[i].classList.add("asiakirjat-current-change");
            }

            // Scroll first element into view
            group[0].scrollIntoView({ behavior: "smooth", block: "center" });

            currentChangeIndex = index;

            // Update counter
            var counter = document.getElementById("asiakirjat-diff-change-counter");
            if (counter) {
                counter.textContent = (index + 1) + " / " + diffChanges.length;
            }
        }

        function nextChange() {
            if (diffChanges.length === 0) return;
            var next = (currentChangeIndex + 1) % diffChanges.length;
            scrollToChange(next);
        }

        function prevChange() {
            if (diffChanges.length === 0) return;
            var prev = currentChangeIndex <= 0 ? diffChanges.length - 1 : currentChangeIndex - 1;
            scrollToChange(prev);
        }

        // Show diff indicator banner
        function showDiffIndicator(targetVersion, hasChanges, changeCount) {
            var indicator = document.getElementById("asiakirjat-diff-indicator");
            var fromVersion = document.getElementById("asiakirjat-diff-from-version");
            var changeInfo = document.getElementById("asiakirjat-diff-change-info");
            var nav = document.getElementById("asiakirjat-diff-nav");

            if (indicator && fromVersion) {
                if (hasChanges) {
                    fromVersion.textContent = targetVersion;
                } else {
                    fromVersion.innerHTML = targetVersion + ' <em>(no changes on this page)</em>';
                }

                // Show/hide change info and nav
                if (changeInfo) {
                    if (hasChanges && changeCount > 0) {
                        changeInfo.textContent = changeCount + " change" + (changeCount !== 1 ? "s" : "") + " found";
                        changeInfo.style.display = "";
                    } else {
                        changeInfo.style.display = "none";
                    }
                }
                if (nav) {
                    nav.style.display = (hasChanges && changeCount > 0) ? "" : "none";
                }

                // Position indicator below the main overlay
                indicator.style.top = overlay.offsetHeight + "px";
                indicator.style.display = "flex";
                // Update body margin to account for both bars
                document.body.style.marginTop = (overlay.offsetHeight + indicator.offsetHeight) + "px";
            }
            diffModeActive = true;
        }

        // Exit diff mode
        function exitDiffMode() {
            if (originalContentHtml && contentContainer) {
                contentContainer.innerHTML = originalContentHtml;
                originalContentHtml = null;
            }
            var indicator = document.getElementById("asiakirjat-diff-indicator");
            if (indicator) {
                indicator.style.display = "none";
            }
            // Reset body margin
            document.body.style.marginTop = overlay.offsetHeight + "px";
            // Reset compare select
            var cmpSelect = document.getElementById("asiakirjat-compare-select");
            if (cmpSelect) {
                cmpSelect.value = "";
            }
            diffModeActive = false;
            contentContainer = null;
            diffChanges = [];
            currentChangeIndex = -1;

            // Remove ?compare= from URL
            var params = new URLSearchParams(window.location.search);
            if (params.has("compare")) {
                params.delete("compare");
                var newUrl = window.location.pathname + (params.toString() ? "?" + params.toString() : "") + window.location.hash;
                history.replaceState(null, "", newUrl);
            }
        }

        // Show loading state
        function showLoading() {
            var loadingDiv = document.createElement("div");
            loadingDiv.id = "asiakirjat-diff-loading-overlay";
            loadingDiv.innerHTML = '<div class="ao-loading-spinner">Computing diff...</div>';
            document.body.appendChild(loadingDiv);
        }

        function hideLoading() {
            var loading = document.getElementById("asiakirjat-diff-loading-overlay");
            if (loading) loading.remove();
        }

        // Show error message
        function showError(message) {
            var indicator = document.getElementById("asiakirjat-diff-indicator");
            var fromVersion = document.getElementById("asiakirjat-diff-from-version");
            if (indicator && fromVersion) {
                fromVersion.innerHTML = '<span style="color: #dc2626;">' + message + '</span>';
                indicator.style.display = "flex";
                document.body.style.marginTop = (overlay.offsetHeight + indicator.offsetHeight) + "px";
            }
            diffModeActive = true;
        }

        // Strip elements that shouldn't be diffed (theme-agnostic)
        function sanitizeForDiff(html) {
            var parser = new DOMParser();
            var doc = parser.parseFromString('<div>' + html + '</div>', 'text/html');
            var root = doc.body.firstChild;

            // Remove elements that cause noise in diffs
            var removeSelectors = [
                'nav', 'script', 'style',
                '.breadcrumb', '.headerlink', '.anchor-link',
                '.toctree-wrapper', '[aria-label="breadcrumbs"]'
            ];
            removeSelectors.forEach(function(sel) {
                var els = root.querySelectorAll(sel);
                for (var i = 0; i < els.length; i++) {
                    els[i].parentNode.removeChild(els[i]);
                }
            });

            return root.innerHTML;
        }

        compareSelect.addEventListener("change", function() {
            var targetVersion = compareSelect.value;
            if (!targetVersion) {
                if (diffModeActive) {
                    exitDiffMode();
                }
                return;
            }

            // Block diff for PDF versions
            var targetOpt = compareSelect.querySelector('option[value="' + CSS.escape(targetVersion) + '"]');
            if (currentIsPdf || (targetOpt && targetOpt.getAttribute("data-content-type") === "pdf")) {
                showError("Diff unavailable for PDF versions");
                return;
            }

            // Find content container
            contentContainer = findContentContainer();
            if (!contentContainer) {
                showError("Could not find content area");
                return;
            }

            // Store original content for toggle
            if (!originalContentHtml) {
                originalContentHtml = contentContainer.innerHTML;
            }

            // Get current document path
            var path = window.location.pathname;
            var prefix = basePath + "/project/" + slug + "/" + current;
            var suffix = path.substring(prefix.length);

            // Build URL for target version
            var targetUrl = basePath + "/project/" + slug + "/" + targetVersion + suffix;
            var containerSelectors = getSelectorsForElement(contentContainer);

            showLoading();

            // Fetch target version
            fetch(targetUrl)
                .then(function(r) {
                    if (!r.ok) throw new Error("Page not found in " + targetVersion);
                    return r.text();
                })
                .then(function(targetHtml) {
                    var currentContent = sanitizeForDiff(originalContentHtml);
                    var targetContent = extractContentFromHtml(targetHtml, containerSelectors);

                    if (!targetContent) {
                        throw new Error("Could not extract content from " + targetVersion + " (different page structure?)");
                    }

                    targetContent = sanitizeForDiff(targetContent);

                    // Check if htmldiff is available
                    if (typeof htmldiff !== 'function') {
                        throw new Error("Diff library not loaded");
                    }

                    // Check if content is identical
                    var normalizedCurrent = currentContent.replace(/\s+/g, ' ').trim();
                    var normalizedTarget = targetContent.replace(/\s+/g, ' ').trim();
                    var hasChanges = normalizedCurrent !== normalizedTarget;

                    if (hasChanges) {
                        // Compute HTML diff (target = old version, current = new version)
                        try {
                            var diffHtml = htmldiff(targetContent, currentContent);

                            // Guard: if diff output is suspiciously short, fall back
                            var minExpected = Math.min(currentContent.length, targetContent.length) * 0.1;
                            if (diffHtml.length < minExpected) {
                                throw new Error("Diff produced unusable output");
                            }

                            contentContainer.innerHTML = diffHtml;
                        } catch (diffErr) {
                            // Fall back to showing current version with warning
                            contentContainer.innerHTML =
                                '<div class="htmldiff-warning">Could not compute diff: ' +
                                diffErr.message + '. Showing current version.</div>' +
                                currentContent;
                        }
                    }

                    // Collect changes for navigation
                    diffChanges = hasChanges ? collectChanges(contentContainer) : [];
                    currentChangeIndex = -1;

                    showDiffIndicator(targetVersion, hasChanges, diffChanges.length);
                    hideLoading();

                    // Add ?compare= to URL
                    var params = new URLSearchParams(window.location.search);
                    params.set("compare", targetVersion);
                    history.replaceState(null, "", window.location.pathname + "?" + params.toString() + window.location.hash);
                })
                .catch(function(err) {
                    hideLoading();
                    showError("Error: " + err.message);
                    // Restore original on error
                    if (originalContentHtml && contentContainer) {
                        contentContainer.innerHTML = originalContentHtml;
                        originalContentHtml = null;
                    }
                    contentContainer = null;

                    // Remove ?compare= from URL on error
                    var errParams = new URLSearchParams(window.location.search);
                    if (errParams.has("compare")) {
                        errParams.delete("compare");
                        var errUrl = window.location.pathname + (errParams.toString() ? "?" + errParams.toString() : "") + window.location.hash;
                        history.replaceState(null, "", errUrl);
                    }
                });
        });

        // Bind exit button
        if (exitDiffBtn) {
            exitDiffBtn.addEventListener("click", exitDiffMode);
        }

        // Keyboard shortcuts for diff mode
        document.addEventListener("keydown", function(e) {
            if (e.key === "Escape" && diffModeActive) {
                exitDiffMode();
                return;
            }
            if (!diffModeActive) return;
            var tag = document.activeElement && document.activeElement.tagName;
            if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
            if (e.key === "n") {
                nextChange();
            } else if (e.key === "p") {
                prevChange();
            }
        });

        // Button click handlers for diff navigation
        var prevBtn = document.getElementById("asiakirjat-prev-change");
        var nextBtn = document.getElementById("asiakirjat-next-change");
        if (prevBtn) prevBtn.addEventListener("click", prevChange);
        if (nextBtn) nextBtn.addEventListener("click", nextChange);

        // Link click interception to preserve ?compare= during diff mode
        document.addEventListener("click", function(e) {
            if (!diffModeActive) return;

            var link = e.target.closest("a");
            if (!link) return;

            // Skip overlay elements
            if (link.closest("#asiakirjat-overlay") || link.closest("#asiakirjat-diff-indicator")) return;

            var href = link.getAttribute("href");
            if (!href) return;

            // Skip special links
            if (href.indexOf("javascript:") === 0 || href.indexOf("mailto:") === 0) return;
            if (href.charAt(0) === "#") return;

            // Resolve relative URL
            var resolved = new URL(href, window.location.href);

            // Only intercept same-origin links
            if (resolved.origin !== window.location.origin) return;

            // Only intercept links within the same project/version
            var projectPrefix = basePath + "/project/" + slug + "/" + current;
            if (resolved.pathname.indexOf(projectPrefix) !== 0) return;

            // Get current compare version
            var compareVersion = compareSelect.value;
            if (!compareVersion) return;

            e.preventDefault();

            // Navigate with ?compare= preserved
            var linkParams = new URLSearchParams(resolved.search);
            linkParams.set("compare", compareVersion);
            window.location.href = resolved.pathname + "?" + linkParams.toString() + resolved.hash;
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

            var url = basePath + "/api/search?q=" + encodeURIComponent(q) +
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
                        if (r.page_number > 0) {
                            item.href = r.url + "?search=" + encodeURIComponent(q) + "#page=" + r.page_number;
                        } else {
                            item.href = r.url + "?highlight=" + encodeURIComponent(q);
                        }

                        var title = document.createElement("div");
                        title.className = "ao-search-item-title";
                        var titleText = r.page_title || r.file_path;
                        if (r.page_number > 0) {
                            titleText += " (p. " + r.page_number + ")";
                        }
                        title.textContent = titleText;
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
                        viewAll.href = basePath + "/search?q=" + encodeURIComponent(q) +
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

        // Keyboard navigation
        var overlaySelectedIndex = -1;

        function getOverlaySelectableItems() {
            return searchDropdown.querySelectorAll("a");
        }

        function updateOverlaySelection() {
            var items = getOverlaySelectableItems();
            items.forEach(function(item, i) {
                if (i === overlaySelectedIndex) {
                    item.classList.add("ao-search-item-selected");
                } else {
                    item.classList.remove("ao-search-item-selected");
                }
            });
            if (overlaySelectedIndex >= 0 && items[overlaySelectedIndex]) {
                items[overlaySelectedIndex].scrollIntoView({ block: "nearest" });
            }
        }

        searchInput.addEventListener("input", function() {
            clearTimeout(searchTimer);
            overlaySelectedIndex = -1;
            searchTimer = setTimeout(overlaySearch, 300);
        });

        searchInput.addEventListener("keydown", function(e) {
            var items = getOverlaySelectableItems();
            var visible = searchDropdown.style.display === "block";

            if (e.key === "Escape") {
                searchDropdown.style.display = "none";
                overlaySelectedIndex = -1;
                return;
            }

            if (!visible || items.length === 0) return;

            if (e.key === "ArrowDown") {
                e.preventDefault();
                overlaySelectedIndex = (overlaySelectedIndex + 1) % items.length;
                updateOverlaySelection();
            } else if (e.key === "ArrowUp") {
                e.preventDefault();
                overlaySelectedIndex = overlaySelectedIndex <= 0 ? items.length - 1 : overlaySelectedIndex - 1;
                updateOverlaySelection();
            } else if (e.key === "Enter") {
                if (overlaySelectedIndex >= 0 && items[overlaySelectedIndex]) {
                    e.preventDefault();
                    items[overlaySelectedIndex].click();
                }
            }
        });

        document.addEventListener("click", function(e) {
            if (!searchInput.contains(e.target) && !searchDropdown.contains(e.target)) {
                searchDropdown.style.display = "none";
                overlaySelectedIndex = -1;
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
