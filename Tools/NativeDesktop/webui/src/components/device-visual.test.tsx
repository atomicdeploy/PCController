import { render, screen } from "@testing-library/preact";
import { describe, expect, it } from "vitest";
import { DeviceVisual } from "./device-visual";

describe("DeviceVisual", () => {
  it("exposes state through one accessible image label", () => {
    const { rerender } = render(
      <DeviceVisual state="on" offline={false} busy={false} />
    );
    expect(screen.getByRole("img", { name: "Device is on" })).toBeTruthy();

    rerender(<DeviceVisual state="off" offline busy={false} />);
    expect(screen.getByRole("img", { name: "Device is offline" })).toBeTruthy();
  });
});
