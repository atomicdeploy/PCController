export type BuildEdition = "develop" | "production";

export const buildEdition: BuildEdition =
  import.meta.env.VITE_PCCONTROLLER_DESKTOP_BUILD_EDITION === "production"
    ? "production"
    : "develop";

export const developerTools = buildEdition === "develop";
