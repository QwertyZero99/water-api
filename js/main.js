//
import { createMap } from "./map.js";

createMap();
const bgtbox = document.getElementById("bgtbox");
const toggleBtn = document.getElementById("toggle-btn");
const sidebar = document.getElementById("sidebar");
toggleBtn.textContent = sidebar.classList.contains("collapsed") ? "›" : "‹";

toggleBtn.addEventListener("click", () => {
    sidebar.classList.toggle("collapsed");
    toggleBtn.classList.toggle("collapsed");

    if (sidebar.classList.contains("collapsed")) {
        toggleBtn.textContent = "›";
    } else {
        toggleBtn.textContent = "‹";
    }
});
const child = document.querySelector(".child");

child.classList.toggle("moved");
const child2 = document.querySelector(".child2");

child2.classList.toggle("moved");