const toggleBtn = document.getElementById("toggle-btn");
const sidebar = document.getElementById("sidebar");

if (toggleBtn && sidebar) {
    toggleBtn.textContent =
        sidebar.classList.contains("collapsed") ? "›" : "‹";

    toggleBtn.addEventListener("click", () => {
        sidebar.classList.toggle("collapsed");
        toggleBtn.classList.toggle("collapsed");

        toggleBtn.textContent =
            sidebar.classList.contains("collapsed") ? "›" : "‹";
    });

    const sidebarLinks = document.querySelectorAll(".sidebar-nav a");

    sidebarLinks.forEach((link) => {
        link.addEventListener("click", (event) => {
            event.preventDefault();

            const destination = link.href;

            sidebar.classList.add("collapsed");
            toggleBtn.classList.add("collapsed");
            toggleBtn.textContent = "›";

            setTimeout(() => {
                window.location.href = destination;
            }, 300);
        });
    });
}