import { render, screen } from "@testing-library/preact";
import { describe, expect, it } from "vitest";
import { LampVisual } from "./lamp-visual";

describe("LampVisual", () => {
  it("exposes state through one accessible image label", () => {
    const { rerender } = render(
      <LampVisual state="on" offline={false} busy={false} />
    );
    expect(screen.getByRole("img", { name: "Lamp is on" })).toBeTruthy();

    rerender(<LampVisual state="off" offline busy={false} />);
    expect(screen.getByRole("img", { name: "Lamp is offline" })).toBeTruthy();
  });
});
