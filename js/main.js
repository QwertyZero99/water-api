import { createMap } from "./map.js";

const mapElement = document.getElementById("map");

if (mapElement) {
  createMap();
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

const observer = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add("visible");
      }
    });
  },
  {
    threshold: 0.2,
  },
);

revealElements.forEach((element) => {
  observer.observe(element);
});
