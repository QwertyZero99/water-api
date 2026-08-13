import { createMap } from "./map.js";

const mapElement = document.getElementById("map");

if (mapElement) {
    createMap();
}


const toggleBtn = document.getElementById("toggle-btn");
const sidebar = document.getElementById("sidebar");

if (toggleBtn && sidebar) {


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
}

const child = document.querySelector(".child");
const child2 = document.querySelector(".child2");

window.addEventListener("load", () => {

    if (child) {
        child.classList.add("moved");
    }

    if (child2) {
        child2.classList.add("moved");
    }

});
const revealElements = document.querySelectorAll(".reveal");

const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {

        if (entry.isIntersecting) {
            entry.target.classList.add("visible");
        }

    });
}, {
    threshold: 0.2
});

revealElements.forEach((element) => {
    observer.observe(element);
});