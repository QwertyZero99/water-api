import { createMap } from "./map.js";

createMap();

const toggleBtn = document.getElementById("toggle-btn");
const sidebar = document.getElementById("sidebar");
const bgtbox = document.getElementById("bgtbox");

if (toggleBtn) {
    toggleBtn.addEventListener("click", () => {
        sidebar.classList.toggle("collapsed");
        
        if (sidebar.classList.contains("collapsed")) {
            bgtbox.style.marginLeft = "0";
            bgtbox.style.width = "100%";
            toggleBtn.style.left = "15px";
        } else {
            bgtbox.style.marginLeft = "350px";
            bgtbox.style.width = "calc(100% - 350px)";
            toggleBtn.style.left = "365px";
        }
    });
} else {
    console.error("Toggle button not found!");
}