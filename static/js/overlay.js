(function() {
    "use strict";

    var select = document.getElementById("asiakirjat-version-select");
    if (!select) return;

    var slug = select.getAttribute("data-slug");
    var current = select.getAttribute("data-current");

    // Fetch versions from API
    fetch("/api/project/" + encodeURIComponent(slug) + "/versions")
        .then(function(resp) { return resp.json(); })
        .then(function(versions) {
            // Clear and rebuild options
            select.innerHTML = "";
            versions.forEach(function(v) {
                var opt = document.createElement("option");
                opt.value = v.tag;
                opt.textContent = v.tag;
                if (v.tag === current) {
                    opt.selected = true;
                }
                select.appendChild(opt);
            });
        })
        .catch(function(err) {
            console.error("Failed to load versions:", err);
        });

    // Handle version switch
    select.addEventListener("change", function() {
        var newVersion = select.value;
        if (newVersion === current) return;

        // Preserve the current path within the doc
        var path = window.location.pathname;
        var prefix = "/project/" + slug + "/" + current;
        var suffix = path.substring(prefix.length);

        window.location.href = "/project/" + slug + "/" + newVersion + suffix;
    });
})();
