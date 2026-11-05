import { fireEvent, render, screen } from "@testing-library/preact";
import { describe, expect, it, vi } from "vitest";
import { TextField } from "./controls";

describe("text field input modes", () => {
  it.each(["text", "url", "password"] as const)(
    "retains labeling, direction and editing for %s",
    (type) => {
      const onInput = vi.fn();
      const { container, rerender } = render(
        <TextField label="Endpoint" type={type} value="initial" onInput={onInput} />
      );
      const input = screen.getByLabelText("Endpoint") as HTMLInputElement;
      expect(container.querySelectorAll("input")).toHaveLength(1);
      expect(input.type).toBe(type);
      expect(input.value).toBe("initial");
      expect(input.dir).toBe(type === "text" ? "" : "ltr");
      fireEvent.input(input, { target: { value: "edited" } });
      expect(onInput).toHaveBeenLastCalledWith("edited");

      rerender(
        <TextField label="Endpoint" type={type} value="saved" readOnly dir="rtl" autoComplete="off" />
      );
      const updated = screen.getByLabelText("Endpoint") as HTMLInputElement;
      expect(updated.value).toBe("saved");
      expect(updated.readOnly).toBe(true);
      expect(updated.dir).toBe("rtl");
      expect(updated.autocomplete).toBe("off");
    }
  );
});
