import { createMap } from "./map.js";

const mapController = await createMap();

const resetButton = document.getElementById("resetZoom");

const button2D = document.getElementById("view2D");

const button3D = document.getElementById("view3D");

if (resetButton) {
  resetButton.addEventListener("click", () => {
    mapController.resetView();
  });
}

function updateModeButtons(mode) {
  const is2D = mode === "2d";

  if (button2D) {
    button2D.classList.toggle("active", is2D);

    button2D.setAttribute(
      "aria-pressed",
      String(is2D),
    );
  }

  if (button3D) {
    button3D.classList.toggle("active", !is2D);

    button3D.setAttribute(
      "aria-pressed",
      String(!is2D),
    );
  }
}

if (button2D) {
  button2D.addEventListener("click", () => {
    mapController.setViewMode("2d");

    updateModeButtons("2d");
  });
}

if (button3D) {
  button3D.addEventListener("click", () => {
    mapController.setViewMode("3d");

    updateModeButtons("3d");
  });
}

const revealElements = document.querySelectorAll(".reveal");

const observer = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add("visible");

        observer.unobserve(entry.target);
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
