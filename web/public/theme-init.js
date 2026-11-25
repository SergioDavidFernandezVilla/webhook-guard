(function () {
  try {
    var theme = localStorage.getItem("wg.theme");
    if (!theme) {
      theme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    document.documentElement.classList.toggle("dark", theme === "dark");
    var locale = localStorage.getItem("wg.locale");
    if (!locale) {
      locale = (navigator.language || "en").toLowerCase().indexOf("es") === 0 ? "es" : "en";
    }
    document.documentElement.lang = locale;
  } catch (error) {
    /* first paint falls back to the light theme and English */
  }
})();
