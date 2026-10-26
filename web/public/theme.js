// Apply the saved theme before the first paint, including the login screen.
// This same-origin file works with the panel's script-src 'self' policy.
try {
  const dark = localStorage.getItem("theme") === "dark";
  document.documentElement.classList.toggle("dark", dark);
  document.querySelector('meta[name="theme-color"]').content = dark
    ? "#101114"
    : "#f6f7f8";
} catch {
  // Storage can be unavailable; the default remains light.
}
