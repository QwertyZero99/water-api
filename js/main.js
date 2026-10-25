
import { createMap } from "./map.js";

const mapElement = document.getElementById("map");

if (mapElement) {
  createMap();
}

// Preserve the existing hero animation.
window.addEventListener("load", () => {
  document.querySelector(".child")?.classList.add("moved");
  document.querySelector(".child2")?.classList.add("moved");
  document.querySelector(".hero-description")?.classList.add("moved");
  document.querySelector(".hero-actions")?.classList.add("moved");
  document.querySelector(".hero-instruction")?.classList.add("moved");
});

// Reveal homepage sections as the user scrolls.
const revealElements = document.querySelectorAll(".reveal");

if ("IntersectionObserver" in window) {
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add("visible");
          observer.unobserve(entry.target);
        }
      }
    },
    {
      threshold: 0.12
    }
  );

  revealElements.forEach((element) => {
    observer.observe(element);
  });
} else {
  revealElements.forEach((element) => {
    element.classList.add("visible");
  });
}
