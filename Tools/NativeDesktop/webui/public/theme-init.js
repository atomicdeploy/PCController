(function () {
  "use strict";

  var requested = "system";
  try {
    var stored = localStorage.getItem("rayanlamp.web.theme");
    if (stored === "light" || stored === "dark" || stored === "system") {
      requested = stored;
    }
  } catch (_) {
    // Storage may be unavailable; system mode remains a safe first paint.
  }

  var prefersLight =
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: light)").matches;
  var resolved =
    requested === "system"
      ? prefersLight
        ? "light"
        : "dark"
      : requested;
  var root = document.documentElement;
  root.dataset.theme = requested;
  root.dataset.resolvedTheme = resolved;
  root.style.colorScheme = resolved;

  var colorScheme = document.querySelector('meta[name="color-scheme"]');
  if (colorScheme) colorScheme.setAttribute("content", resolved);
  var themeColor = document.querySelector('meta[name="theme-color"]');
  if (themeColor) {
    themeColor.setAttribute(
      "content",
      resolved === "light" ? "#eef4f8" : "#0b1724"
    );
  }

  var locale = "auto";
  try {
    var storedLocale = localStorage.getItem("rayanlamp.web.locale");
    if (
      storedLocale === "auto" ||
      storedLocale === "en" ||
      storedLocale === "fa"
    ) {
      locale = storedLocale;
    }
  } catch (_) {
    // Configuration takes over after bootstrap; avoid a first-paint flash.
  }
  var languages = navigator.languages && navigator.languages.length
    ? navigator.languages
    : [navigator.language || "en"];
  var resolvedLocale = locale === "auto"
    ? languages.some(function (language) {
        return String(language).toLowerCase().indexOf("fa") === 0;
      })
      ? "fa"
      : "en"
    : locale;
  root.lang = resolvedLocale === "fa" ? "fa-IR" : "en";
  root.dir = resolvedLocale === "fa" ? "rtl" : "ltr";
  root.dataset.locale = locale;
  root.dataset.resolvedLocale = resolvedLocale;
})();
