import { render } from "preact";
import "virtual:rayanlamp-design.css";
import "./styles/app.css";
import { App } from "./app";
import { browserConsole } from "./browser-console";
import { buildEdition } from "./build-edition";
import { applyWebLocale } from "./i18n";

const root = document.getElementById("app");
if (!root) {
  throw new Error("Rayan Lamp web root is missing.");
}

browserConsole.start({
  source: import.meta.env.VITE_RAYANLAMP_STATIC_EXPORT === "true"
    ? "static-web"
    : "bundled-web",
  action: buildEdition
});
applyWebLocale(document.documentElement.dataset.locale);
render(<App />, root);
