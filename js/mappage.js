import { createMap } from "./map.js";

createMap();

const toggleBtn = document.getElementById("toggle-btn");
const sidebar = document.getElementById("sidebar");

toggleBtn.textContent =
    sidebar.classList.contains("collapsed") ? "›" : "‹";

toggleBtn.addEventListener("click", () => {

    sidebar.classList.toggle("collapsed");
    toggleBtn.classList.toggle("collapsed");

    if (sidebar.classList.contains("collapsed")) {
        toggleBtn.textContent = "›";
    } else {
        toggleBtn.textContent = "‹";
    }

});