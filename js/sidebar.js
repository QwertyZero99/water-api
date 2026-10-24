
const toggleBtn = document.getElementById("toggle-btn");
const sidebar = document.getElementById("sidebar");

if (toggleBtn && sidebar) {
  // Set the initial toggle arrow.
  toggleBtn.textContent = sidebar.classList.contains("collapsed")
    ? "›"
    : "‹";

  // Open and close the sidebar.
  toggleBtn.addEventListener("click", () => {
    sidebar.classList.toggle("collapsed");
    toggleBtn.classList.toggle("collapsed");

    toggleBtn.textContent = sidebar.classList.contains("collapsed")
      ? "›"
      : "‹";
  });

  // Let sidebar links navigate normally.
  // No preventDefault(), timeout, or custom redirects.
}
