(function() {
    "use strict";

    var input = document.getElementById("search-input");
    if (!input) return;

    var cards = document.querySelectorAll(".project-card");

    input.addEventListener("input", function() {
        var query = input.value.toLowerCase().trim();

        cards.forEach(function(card) {
            var name = card.getAttribute("data-name") || "";
            var slug = card.getAttribute("data-slug") || "";
            var desc = (card.querySelector(".project-card-desc") || {}).textContent || "";

            if (!query || name.indexOf(query) !== -1 || slug.indexOf(query) !== -1 || desc.toLowerCase().indexOf(query) !== -1) {
                card.classList.remove("hidden");
            } else {
                card.classList.add("hidden");
            }
        });
    });
})();
