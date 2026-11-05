export type BuildEdition = "develop" | "production";

export const buildEdition: BuildEdition =
  import.meta.env.VITE_RAYANLAMP_BUILD_EDITION === "production"
    ? "production"
    : "develop";

export const developerTools = buildEdition === "develop";
