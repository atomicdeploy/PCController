/**
 * Restrained, capability-aware vibration feedback for the mobile Web UI.
 *
 * This surface intentionally accepts only normalized semantic cues. It never
 * receives or retains lamp frames, message bodies, terminal data, or other raw
 * payloads.
 */

export type WebHapticCue =
  | "command.start"
  | "command.success"
  | "command.error"
  | "message.received"
  | "audio.cue";

export interface WebHapticsRuntime {
  vibrate?: (pattern: number | number[]) => boolean;
  isDocumentVisible: () => boolean;
  prefersReducedMotion: () => boolean;
}

const fixedPatterns: Record<
  Exclude<WebHapticCue, "audio.cue">,
  number | readonly number[]
> = {
  "command.start": 12,
  "command.success": [15, 35, 24],
  "command.error": [28, 42, 28],
  "message.received": [18, 55, 18]
};

export class WebHaptics {
  constructor(
    private readonly runtime: WebHapticsRuntime = browserHapticsRuntime
  ) {}

  get supported(): boolean {
    return typeof this.runtime.vibrate === "function";
  }

  trigger(cue: Exclude<WebHapticCue, "audio.cue">): boolean {
    return this.play(fixedPatterns[cue]);
  }

  audioCue(count: number): boolean {
    if (!Number.isFinite(count) || count < 1) return false;
    const pulseCount = Math.min(3, Math.floor(count));
    const pattern: number[] = [];
    for (let index = 0; index < pulseCount; index += 1) {
      if (index > 0) pattern.push(65);
      pattern.push(20);
    }
    return this.play(pattern);
  }

  private play(pattern: number | readonly number[]): boolean {
    const vibrate = this.runtime.vibrate;
    if (
      typeof vibrate !== "function" ||
      !safeCondition(this.runtime.isDocumentVisible, false) ||
      safeCondition(this.runtime.prefersReducedMotion, true)
    ) {
      return false;
    }

    try {
      // Give the browser its own array: a host implementation must not be able
      // to mutate the shared pattern constants.
      return Boolean(
        vibrate(typeof pattern === "number" ? pattern : Array.from(pattern))
      );
    } catch {
      // Unsupported, policy-blocked, or partially implemented vibration APIs
      // should never affect lamp controls.
      return false;
    }
  }
}

function safeCondition(
  read: () => boolean,
  fallback: boolean
): boolean {
  try {
    return read();
  } catch {
    return fallback;
  }
}

const browserHapticsRuntime: WebHapticsRuntime = {
  vibrate:
    typeof navigator !== "undefined" &&
    typeof navigator.vibrate === "function"
      ? (pattern) => navigator.vibrate(pattern)
      : undefined,
  isDocumentVisible: () =>
    typeof document !== "undefined" &&
    document.visibilityState === "visible",
  prefersReducedMotion: () =>
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
};

export const webHaptics = new WebHaptics();
