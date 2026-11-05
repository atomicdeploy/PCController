import type { LampState } from "../contracts";
import { t } from "../i18n";

export function LampVisual({
  state,
  offline,
  busy
}: {
  state: LampState;
  offline: boolean;
  busy: boolean;
}) {
  const visualState = offline ? "offline" : state;
  return (
    <div
      class={`lamp-visual lamp-${visualState}${busy ? " is-busy" : ""}`}
      role="img"
      aria-label={
        offline
          ? t("Lamp is offline")
          : state === "on"
            ? t("Lamp is on")
            : state === "off"
              ? t("Lamp is off")
              : t("Lamp state is unknown")
      }
    >
      <div class="lamp-aura" />
      <svg viewBox="0 0 512 512" focusable="false" aria-hidden="true">
        <defs>
          <linearGradient id="web-lamp-glass" x1="0" y1="0" x2=".78" y2="1">
            <stop class="lamp-stop-top" offset="0" />
            <stop class="lamp-stop-core" offset=".56" />
            <stop class="lamp-stop-bottom" offset="1" />
          </linearGradient>
          <linearGradient id="web-lamp-base" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" stop-color="#7d8c9b" />
            <stop offset=".45" stop-color="#465462" />
            <stop offset="1" stop-color="#202b36" />
          </linearGradient>
          <filter id="web-lamp-glow" x="-80%" y="-80%" width="260%" height="260%">
            <feGaussianBlur stdDeviation="15" />
          </filter>
        </defs>
        <g class="lamp-rays">
          <path d="M256 15v35" />
          <path d="m124 57 25 31" />
          <path d="m50 173 41 8" />
          <path d="m388 57-25 31" />
          <path d="m462 173-41 8" />
          <path d="m70 305 37-17" />
          <path d="m442 305-37-17" />
        </g>
        <path
          class="lamp-glow"
          d="M256 66c-79 0-136 60-136 136 0 55 29 89 57 117 17 17 24 35 25 57h108c1-22 8-40 25-57 28-28 57-62 57-117 0-76-57-136-136-136Z"
          filter="url(#web-lamp-glow)"
        />
        <path
          class="lamp-glass"
          d="M256 66c-79 0-136 60-136 136 0 55 29 89 57 117 17 17 24 35 25 57h108c1-22 8-40 25-57 28-28 57-62 57-117 0-76-57-136-136-136Z"
          fill="url(#web-lamp-glass)"
        />
        <path class="lamp-shine" d="M185 151c15-31 42-51 77-55" />
        <path class="lamp-filament" d="m218 281 38-49 38 49M256 232v98" />
        <path
          class="lamp-base"
          d="M197 369h118v30c0 10-8 18-18 18h-82c-10 0-18-8-18-18Z"
          fill="url(#web-lamp-base)"
        />
        <path class="lamp-base-lines" d="M205 390h102M209 407h94" />
        <path class="lamp-foot" d="M222 418h68v10c0 22-15 38-34 38s-34-16-34-38Z" />
      </svg>
      {offline && (
        <span class="offline-badge" aria-hidden="true">
          <span />
        </span>
      )}
    </div>
  );
}
