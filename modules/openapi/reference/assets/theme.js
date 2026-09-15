// Applies the reader's chosen theme before the page paints.
try {
  const theme = localStorage.getItem("theme");
  if (theme === "light" || theme === "dark") document.documentElement.dataset.theme = theme;
} catch { /* storage unavailable: follow the system */ }
