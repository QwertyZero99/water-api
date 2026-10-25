
document.addEventListener("DOMContentLoaded", () => {
  const elements = document.querySelectorAll(
    ".info-page .hero, .info-page section"
  );

  const reduceMotion = window.matchMedia(
    "(prefers-reduced-motion: reduce)"
  ).matches;

  if (!reduceMotion && "IntersectionObserver" in window) {
    elements.forEach(element => {
      element.classList.add("reveal");
    });

    const observer = new IntersectionObserver(entries => {
      entries.forEach(entry => {
        if (!entry.isIntersecting) return;

        entry.target.classList.add("visible");
        observer.unobserve(entry.target);
      });
    }, {
      threshold: 0.08,
      rootMargin: "0px 0px -30px 0px"
    });

    elements.forEach(element => observer.observe(element));
  }

  // Reveal the correct explanation when following
  // a direct link such as /info#ph or /info#streamflow.
  function revealLinkedSection() {
    if (!window.location.hash) return;

    let id;
    try {
      id = decodeURIComponent(window.location.hash.slice(1));
    } catch {
      return;
    }

    const target = document.getElementById(id);
    if (!target) return;

    const section = target.closest("section");

    if (section) {
      section.classList.add("visible");
    }

    target.classList.add("visible");
  }

  revealLinkedSection();

  window.addEventListener("hashchange", revealLinkedSection);
});
