
const menuButton = document.getElementById("aquany-menu-button");
const dropdown = document.getElementById("aquany-dropdown");

if (menuButton && dropdown) {
  function setMenuOpen(open) {
    dropdown.hidden = !open;
    menuButton.setAttribute("aria-expanded", String(open));
    menuButton.setAttribute(
      "aria-label",
      open ? "Close navigation menu" : "Open navigation menu"
    );
  }

  menuButton.addEventListener("click", () => {
    setMenuOpen(dropdown.hidden);
  });

  document.addEventListener("click", (event) => {
    if (!event.target.closest(".aquany-navigation")) {
      setMenuOpen(false);
    }
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !dropdown.hidden) {
      setMenuOpen(false);
      menuButton.focus();
    }
  });

  const currentPath = window.location.pathname;

  dropdown.querySelectorAll("a").forEach((link) => {
    if (link.getAttribute("href") === currentPath) {
      link.setAttribute("aria-current", "page");
    }
  });
}
