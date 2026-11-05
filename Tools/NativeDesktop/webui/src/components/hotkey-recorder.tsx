import { Fragment, type JSX } from "preact";
import { useEffect, useId, useRef, useState } from "preact/hooks";
import { Button } from "./controls";
import { Icon } from "./icon";
import { t } from "../i18n";

type Modifier = "Ctrl" | "Alt" | "Shift" | "Win";

export type HotkeyConflict = {
  label: string;
  value: string;
};

export type HotkeyRecorderProps = {
  label: string;
  value: string;
  conflicts?: readonly HotkeyConflict[];
  captureActive?: boolean;
  captureError?: string;
  onCaptureStart?: () =>
    | RecorderCaptureStartResult
    | Promise<RecorderCaptureStartResult>;
  onCaptureStop?: (
    reason: "cancel" | "complete"
  ) => void | string | Promise<void | string>;
  onProbe?: (
    chord: string
  ) => RecorderProbeResult | Promise<RecorderProbeResult>;
  onCommit: (value: string) => void | Promise<void>;
};

export type RecorderCaptureStartResult = {
  active: boolean;
  message?: string;
};

export type RecorderProbeResult = {
  available: boolean;
  message?: string;
};

type RecorderOperation = "" | "probing" | "applying";

const modifierOrder: readonly Modifier[] = ["Ctrl", "Alt", "Shift", "Win"];

const modifierAliases: Record<string, Modifier> = {
  ctrl: "Ctrl",
  control: "Ctrl",
  alt: "Alt",
  shift: "Shift",
  win: "Win",
  windows: "Win",
  meta: "Win",
  super: "Win"
};

const namedKeys: Record<string, string> = {
  backspace: "Backspace",
  back: "Backspace",
  tab: "Tab",
  enter: "Enter",
  return: "Enter",
  pause: "Pause",
  capslock: "CapsLock",
  escape: "Escape",
  esc: "Escape",
  space: "Space",
  pageup: "PageUp",
  pgup: "PageUp",
  prior: "PageUp",
  pagedown: "PageDown",
  pgdn: "PageDown",
  next: "PageDown",
  end: "End",
  home: "Home",
  left: "Left",
  up: "Up",
  right: "Right",
  down: "Down",
  printscreen: "PrintScreen",
  print_screen: "PrintScreen",
  snapshot: "PrintScreen",
  prtsc: "PrintScreen",
  prtscr: "PrintScreen",
  insert: "Insert",
  ins: "Insert",
  delete: "Delete",
  del: "Delete",
  apps: "Apps",
  menu: "Apps",
  contextmenu: "Apps",
  context_menu: "Apps",
  application: "Apps",
  sleep: "Sleep",
  multiply: "Multiply",
  add: "Add",
  separator: "Separator",
  numseparator: "Separator",
  numpadseparator: "Separator",
  subtract: "Subtract",
  decimal: "Decimal",
  divide: "Divide",
  numlock: "NumLock",
  scrolllock: "ScrollLock",
  browserback: "BrowserBack",
  browser_back: "BrowserBack",
  browserforward: "BrowserForward",
  browser_forward: "BrowserForward",
  browserrefresh: "BrowserRefresh",
  browser_refresh: "BrowserRefresh",
  browserstop: "BrowserStop",
  browser_stop: "BrowserStop",
  browsersearch: "BrowserSearch",
  browser_search: "BrowserSearch",
  browserfavorites: "BrowserFavorites",
  browser_favorites: "BrowserFavorites",
  browserhome: "BrowserHome",
  browser_home: "BrowserHome",
  volumemute: "VolumeMute",
  volume_mute: "VolumeMute",
  volumedown: "VolumeDown",
  volume_down: "VolumeDown",
  volumeup: "VolumeUp",
  volume_up: "VolumeUp",
  medianext: "MediaNext",
  media_next: "MediaNext",
  mediaprevious: "MediaPrevious",
  media_previous: "MediaPrevious",
  mediastop: "MediaStop",
  media_stop: "MediaStop",
  mediaplaypause: "MediaPlayPause",
  media_play_pause: "MediaPlayPause",
  launchmail: "LaunchMail",
  launch_mail: "LaunchMail",
  mail: "LaunchMail",
  launchmedia: "LaunchMediaSelect",
  launch_media: "LaunchMediaSelect",
  launchmediaselect: "LaunchMediaSelect",
  media_select: "LaunchMediaSelect",
  launchapp1: "LaunchApp1",
  launch_app1: "LaunchApp1",
  app1: "LaunchApp1",
  launchapp2: "LaunchApp2",
  launch_app2: "LaunchApp2",
  app2: "LaunchApp2",
  semicolon: "Semicolon",
  ";": "Semicolon",
  equal: "Equal",
  equals: "Equal",
  "=": "Equal",
  comma: "Comma",
  ",": "Comma",
  minus: "Minus",
  "-": "Minus",
  period: "Period",
  dot: "Period",
  ".": "Period",
  slash: "Slash",
  "/": "Slash",
  backtick: "Backtick",
  grave: "Backtick",
  "`": "Backtick",
  leftbracket: "LeftBracket",
  left_bracket: "LeftBracket",
  "[": "LeftBracket",
  backslash: "Backslash",
  "\\": "Backslash",
  rightbracket: "RightBracket",
  right_bracket: "RightBracket",
  "]": "RightBracket",
  quote: "Quote",
  apostrophe: "Quote",
  "'": "Quote"
};

for (let number = 0; number <= 9; number += 1) {
  namedKeys[`num${number}`] = `Num${number}`;
  namedKeys[`numpad${number}`] = `Num${number}`;
}

export function canonicalizeHotkey(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return "";

  const modifiers = new Set<Modifier>();
  let key = "";
  for (const rawToken of trimmed.split("+")) {
    const token = rawToken.trim();
    if (!token) return null;

    const modifier = modifierAliases[token.toLowerCase()];
    if (modifier) {
      if (modifiers.has(modifier)) return null;
      modifiers.add(modifier);
      continue;
    }

    if (key) return null;
    key = canonicalKeyName(token) ?? "";
    if (!key) return null;
  }

  if (!key) return null;
  return [
    ...modifierOrder.filter((modifier) => modifiers.has(modifier)),
    key
  ].join("+");
}

export function HotkeyRecorder({
  label,
  value,
  conflicts = [],
  captureActive,
  captureError = "",
  onCaptureStart,
  onCaptureStop,
  onProbe,
  onCommit
}: HotkeyRecorderProps) {
  const identity = useId();
  const statusID = `${identity}-status`;
  const errorID = `${identity}-error`;
  const warningID = `${identity}-warning`;
  const [recording, setRecording] = useState(false);
  const [heldModifiers, setHeldModifiers] = useState<Modifier[]>([]);
  const [recordingError, setRecordingError] = useState("");
  const [sessionError, setSessionError] = useState("");
  const [statusNotice, setStatusNotice] = useState("");
  const [preparing, setPreparing] = useState(false);
  const [operation, setOperation] = useState<RecorderOperation>("");

  const normalizedValue = canonicalizeHotkey(value);
  const controlledIdentity =
    normalizedValue === null ? value.trim() : normalizedValue;
  const controlledIdentityRef = useRef(controlledIdentity);
  controlledIdentityRef.current = controlledIdentity;
  const captureBaseline = useRef<string | null>(null);
  const expectedCommit = useRef<string | null>(null);
  const previousControlledIdentity = useRef(controlledIdentity);
  const requestGeneration = useRef(0);
  const storedError =
    normalizedValue === null
      ? t("This stored shortcut is not supported. Record a replacement or clear it.")
      : reservedHotkeyMessage(normalizedValue)
        ? t("F12 is reserved by Windows. Record a different shortcut or clear it.")
        : conflictMessage(normalizedValue, conflicts);
  const storedWarning =
    !storedError && normalizedValue
      ? riskyHotkeyMessage(normalizedValue)
      : "";
  const lifecycleError = sessionError || captureError;
  const error = recording
    ? recordingError || lifecycleError
    : lifecycleError || storedError;
  const warning = recording || error ? "" : storedWarning;
  const displayedTokens = recording
    ? heldModifiers
    : normalizedValue === null
      ? value.trim()
        ? [value.trim()]
        : []
      : normalizedValue
        ? normalizedValue.split("+")
        : [];

  const stopRecording = (notice = "") => {
    setRecording(false);
    setPreparing(false);
    setOperation("");
    setHeldModifiers([]);
    setRecordingError("");
    setStatusNotice(notice);
    captureBaseline.current = null;
  };

  const endCapture = async (reason: "cancel" | "complete") => {
    if (!onCaptureStop) return;
    try {
      const message = await onCaptureStop(reason);
      if (message) setSessionError(message);
    } catch (error) {
      setSessionError(
        recorderErrorMessage(
          error,
          "The capture session ended with a shortcut restore warning."
        )
      );
    }
  };

  const cancelCapture = (notice = "") => {
    const wasCapturing = recording || preparing || operation !== "";
    requestGeneration.current += 1;
    stopRecording(notice);
    if (wasCapturing) void endCapture("cancel");
  };

  const enterRecording = () => {
    captureBaseline.current = controlledIdentityRef.current;
    setPreparing(false);
    setRecording(true);
    setHeldModifiers([]);
    setRecordingError("");
    setSessionError("");
    setStatusNotice("");
  };

  const resolveCaptureStart = (
    generation: number,
    result: RecorderCaptureStartResult
  ) => {
    if (requestGeneration.current !== generation) return;
    setPreparing(false);
    if (!result.active) {
      if (result.message) setSessionError(result.message);
      return;
    }
    enterRecording();
  };

  const beginCapture = () => {
    if (recording) {
      cancelCapture();
      return;
    }
    const generation = ++requestGeneration.current;
    expectedCommit.current = null;
    setSessionError("");
    setRecordingError("");
    setStatusNotice("");
    if (!onCaptureStart) {
      enterRecording();
      return;
    }

    setPreparing(true);
    let result:
      | RecorderCaptureStartResult
      | Promise<RecorderCaptureStartResult>;
    try {
      result = onCaptureStart();
    } catch (error) {
      setPreparing(false);
      setSessionError(
        recorderErrorMessage(
          error,
          "Rayan Lamp could not prepare shortcut recording."
        )
      );
      return;
    }
    if (isPromiseLike(result)) {
      void result.then(
        (value) => resolveCaptureStart(generation, value),
        (error) => {
          if (requestGeneration.current !== generation) return;
          setPreparing(false);
          setSessionError(
            recorderErrorMessage(
              error,
              "Rayan Lamp could not prepare shortcut recording."
            )
          );
        }
      );
      return;
    }
    resolveCaptureStart(generation, result);
  };

  const commitWithoutProbe = (chord: string) => {
    stopRecording();
    if (chord === controlledIdentityRef.current) return;
    expectedCommit.current = chord;
    try {
      const result = onCommit(chord);
      if (isPromiseLike(result)) {
        void result.catch((error) => {
          setSessionError(
            recorderErrorMessage(error, "The shortcut could not be applied.")
          );
        });
      }
    } catch (error) {
      setSessionError(
        recorderErrorMessage(error, "The shortcut could not be applied.")
      );
    }
  };

  const applyCandidate = async (chord: string, probe: boolean) => {
    const generation = requestGeneration.current;
    setRecordingError("");
    setSessionError("");
    if (probe && onProbe) {
      setOperation("probing");
      let result: RecorderProbeResult;
      try {
        result = await onProbe(chord);
      } catch (error) {
        if (requestGeneration.current !== generation) return;
        setOperation("");
        setSessionError(
          recorderErrorMessage(
            error,
            "Rayan Lamp could not verify that shortcut. Try again."
          )
        );
        return;
      }
      if (requestGeneration.current !== generation) return;
      if (!result.available) {
        setOperation("");
        setRecordingError(
          result.message ||
            "Windows or another application already owns this shortcut."
        );
        return;
      }
    }

    if (requestGeneration.current !== generation) return;
    setOperation("applying");
    expectedCommit.current = chord;
    try {
      await onCommit(chord);
    } catch (error) {
      if (requestGeneration.current !== generation) return;
      setOperation("");
      setSessionError(
        recorderErrorMessage(
          error,
          "The shortcut could not be applied. Press it again to retry."
        )
      );
      return;
    }
    if (requestGeneration.current !== generation) return;

    stopRecording();
    await endCapture("complete");
  };

  const clear = () => {
    if (recording && (onCaptureStart || onCaptureStop || onProbe)) {
      void applyCandidate("", false);
      return;
    }

    const wasRecording = recording;
    if (value.trim()) expectedCommit.current = "";
    stopRecording();
    if (value.trim()) {
      try {
        const result = onCommit("");
        if (isPromiseLike(result)) {
          void result.catch((error) => {
            setSessionError(
              recorderErrorMessage(
                error,
                "The shortcut could not be cleared."
              )
            );
          });
        }
      } catch (error) {
        setSessionError(
          recorderErrorMessage(error, "The shortcut could not be cleared.")
        );
      }
    }
    if (wasRecording) void endCapture("complete");
  };

  useEffect(() => {
    const previous = previousControlledIdentity.current;
    previousControlledIdentity.current = controlledIdentity;
    if (previous === controlledIdentity) return;

    if (
      expectedCommit.current !== null &&
      expectedCommit.current === controlledIdentity
    ) {
      expectedCommit.current = null;
      if (recording) captureBaseline.current = controlledIdentity;
      setStatusNotice("");
      return;
    }
    expectedCommit.current = null;

    if (
      recording &&
      captureBaseline.current !== null &&
      captureBaseline.current !== controlledIdentity
    ) {
      const assignment =
        normalizedValue === null || storedError
          ? t("The new stored shortcut needs attention.")
          : normalizedValue
            ? t("{keys} is now assigned.", {
                keys: announceChord(normalizedValue)
              })
            : t("The shortcut is now disabled.");
      cancelCapture(
        t("Shortcut changed elsewhere. Recording cancelled. {assignment}", {
          assignment
        })
      );
    } else {
      setStatusNotice("");
    }
  }, [controlledIdentity, normalizedValue, recording]);

  useEffect(() => {
    if (
      captureActive !== false ||
      (!recording && !preparing && operation === "")
    ) {
      return;
    }
    requestGeneration.current += 1;
    stopRecording();
  }, [captureActive, operation, preparing, recording]);

  const handleKeyDown = (
    event: JSX.TargetedKeyboardEvent<HTMLButtonElement>
  ) => {
    if (!recording || operation !== "") return;
    event.preventDefault();
    event.stopPropagation();
    if (event.repeat) return;

    if (event.key === "Escape" || event.key === "Esc") {
      cancelCapture();
      return;
    }
    const modifiers = modifiersFromEvent(event);
    if (
      modifiers.length === 0 &&
      (event.key === "Backspace" || event.key === "Delete")
    ) {
      clear();
      return;
    }

    if (isModifierKey(event.key)) {
      setHeldModifiers(modifiers);
      setRecordingError("");
      return;
    }

    const key = keyFromKeyboardEvent(event);
    if (!key) {
      setHeldModifiers(modifiers);
      setRecordingError(
        t("“{key}” is not supported for a global shortcut.", {
          key: event.key || event.code || t("That key")
        })
      );
      return;
    }

    const chord = [...modifiers, key].join("+");
    if (key === "F12") {
      setHeldModifiers(modifiers);
      setRecordingError(t("F12 is reserved by Windows. Choose another shortcut."));
      return;
    }
    const conflict = conflictMessage(chord, conflicts);
    if (conflict) {
      setHeldModifiers(modifiers);
      setRecordingError(conflict);
      return;
    }

    if (onCaptureStart || onCaptureStop || onProbe) {
      void applyCandidate(chord, Boolean(onProbe));
    } else {
      commitWithoutProbe(chord);
      if (onCaptureStop) void endCapture("complete");
    }
  };

  const handleKeyUp = (
    event: JSX.TargetedKeyboardEvent<HTMLButtonElement>
  ) => {
    if (!recording || operation !== "") return;
    event.preventDefault();
    event.stopPropagation();
    if (isModifierKey(event.key)) {
      setHeldModifiers(modifiersFromEvent(event));
    }
  };

  const busy = preparing || operation !== "";
  const captureEngaged = recording || preparing;
  const statusText = preparing
    ? t("Preparing secure shortcut capture…")
    : operation === "probing"
      ? t("Checking whether Windows can register this shortcut…")
      : operation === "applying"
        ? t("Applying the shortcut before restoring global hotkeys…")
        : recording
          ? heldModifiers.length
            ? t("Holding {keys}. Press the main key; Escape cancels.", {
                keys: announceChord(heldModifiers.join("+"))
              })
            : t("Waiting for a shortcut. Escape cancels; Backspace or Delete clears.")
    : (statusNotice ? t(statusNotice) : "") ||
      (normalizedValue === null
        ? t("The stored shortcut needs attention.")
        : normalizedValue
          ? storedError
            ? t("{keys} is stored but unavailable.", {
                keys: announceChord(normalizedValue)
              })
            : t("{keys} is assigned.", {
                keys: announceChord(normalizedValue)
              })
          : t("No shortcut is assigned."));
  const messageID = error ? errorID : warning ? warningID : "";

  return (
    <section
      class={`hotkey-recorder${captureEngaged ? " is-recording" : ""}${busy ? " is-busy" : ""}${error ? " has-error" : ""}${warning ? " has-warning" : ""}`}
      aria-labelledby={`${identity}-label`}
    >
      <div class="hotkey-recorder-heading">
        <span id={`${identity}-label`} class="field-label">
          {t(label)}
        </span>
        <span class="hotkey-recorder-kind">{t("Global shortcut")}</span>
      </div>

      <div
        class="hotkey-recorder-value"
        dir="ltr"
        aria-hidden="true"
      >
        {displayedTokens.length ? (
          displayedTokens.map((token, index) => (
            <Fragment key={`${token}-${index}`}>
              {index > 0 && (
                <span class="hotkey-plus" aria-hidden="true">
                  +
                </span>
              )}
              <kbd class={`hotkey-chip${recording ? " is-live" : ""}`}>
                {token}
              </kbd>
            </Fragment>
          ))
        ) : (
          <span class="hotkey-empty">
            {recording ? t("Press your shortcut…") : t("Not assigned")}
          </span>
        )}
      </div>

      <div class="hotkey-recorder-actions">
        <Button
          type="button"
          kind={captureEngaged ? "primary" : "secondary"}
          class="hotkey-record-button"
          busy={busy}
          disabled={busy}
          icon={
            recording ? (
              <span class="hotkey-recording-dot" aria-hidden="true" />
            ) : (
              <Icon name="keyboard" size={16} />
            )
          }
          aria-label={
            preparing
              ? t("Preparing {label} shortcut recording", { label: t(label) })
              : operation === "probing"
                ? t("Checking {label} shortcut availability", { label: t(label) })
                : operation === "applying"
                  ? t("Applying {label} shortcut", { label: t(label) })
                  : recording
              ? t("Cancel recording for {label}", { label: t(label) })
              : t("Record {label} shortcut", { label: t(label) })
          }
          aria-pressed={captureEngaged}
          aria-describedby={`${statusID}${messageID ? ` ${messageID}` : ""}`}
          onClick={beginCapture}
          onKeyDown={handleKeyDown}
          onKeyUp={handleKeyUp}
          onBlur={(event) => {
            if (!recording || operation !== "") return;
            const next = event.relatedTarget;
            if (next instanceof Element) {
              const currentRecorder =
                event.currentTarget.closest(".hotkey-recorder");
              const nextRecorder = next.closest(".hotkey-recorder");
              if (
                nextRecorder === currentRecorder ||
                next.closest(".hotkey-record-button")
              ) {
                return;
              }
            }
            cancelCapture();
          }}
        >
          {preparing
            ? t("Preparing")
            : operation === "probing"
              ? t("Checking")
              : operation === "applying"
                ? t("Applying")
                : recording
                  ? t("Cancel")
                  : t("Record")}
        </Button>
        <Button
          type="button"
          kind="quiet"
          class="hotkey-clear-button"
          disabled={busy || (!value.trim() && !recording)}
          aria-label={t("Clear {label} shortcut", { label: t(label) })}
          onClick={clear}
        >
          {t("Clear")}
        </Button>
      </div>

      <output
        id={statusID}
        class="hotkey-recorder-status"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        {statusText}
      </output>
      {error && (
        <span id={errorID} class="hotkey-recorder-error" role="alert">
          <Icon name="warning" size={14} />
          {t(error)}
        </span>
      )}
      {warning && (
        <span
          id={warningID}
          class="hotkey-recorder-warning"
          role="note"
        >
          <Icon name="warning" size={14} />
          {t(warning)}
        </span>
      )}
    </section>
  );
}

function canonicalKeyName(token: string): string | null {
  const upper = token.toUpperCase();
  if (/^[A-Z0-9]$/.test(upper)) return upper;

  const functionKey = /^F([1-9]|1[0-9]|2[0-4])$/i.exec(token);
  if (functionKey) return `F${Number(functionKey[1])}`;
  return namedKeys[token.toLowerCase()] ?? null;
}

function conflictMessage(
  value: string,
  conflicts: readonly HotkeyConflict[]
): string {
  const normalized = canonicalizeHotkey(value);
  if (!normalized) return "";

  const match = conflicts.find((candidate) => {
    const other = canonicalizeHotkey(candidate.value);
    return other !== null && other !== "" && other === normalized;
  });
  return match
    ? t("Already assigned to {label}. Choose another shortcut.", {
        label: t(match.label)
      })
    : "";
}

function reservedHotkeyMessage(value: string): string {
  const tokens = value.split("+");
  return tokens[tokens.length - 1] === "F12"
    ? t("F12 is reserved by Windows.")
    : "";
}

function riskyHotkeyMessage(value: string): string {
  const tokens = value.split("+");
  const modifiers = tokens.slice(0, -1);
  return modifiers.length === 0 ||
    (modifiers.length === 1 && modifiers[0] === "Shift")
    ? t("System-wide single-key shortcut — use carefully.")
    : "";
}

function announceChord(value: string): string {
  return value.split("+").join(" plus ");
}

function isPromiseLike<T>(value: T | Promise<T>): value is Promise<T> {
  return Boolean(
    value &&
      typeof value === "object" &&
      "then" in value &&
      typeof value.then === "function"
  );
}

function recorderErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message
    ? error.message
    : fallback;
}

function isModifierKey(key: string): boolean {
  return [
    "Control",
    "Ctrl",
    "Alt",
    "AltGraph",
    "Shift",
    "Meta",
    "OS"
  ].includes(key);
}

function modifiersFromEvent(
  event: Pick<KeyboardEvent, "ctrlKey" | "altKey" | "shiftKey" | "metaKey">
): Modifier[] {
  const modifiers: Modifier[] = [];
  if (event.ctrlKey) modifiers.push("Ctrl");
  if (event.altKey) modifiers.push("Alt");
  if (event.shiftKey) modifiers.push("Shift");
  if (event.metaKey) modifiers.push("Win");
  return modifiers;
}

function keyFromKeyboardEvent(
  event: Pick<KeyboardEvent, "key" | "code">
): string | null {
  const codeLetter = /^Key([A-Z])$/.exec(event.code);
  if (codeLetter) return codeLetter[1];

  const codeDigit = /^Digit([0-9])$/.exec(event.code);
  if (codeDigit) return codeDigit[1];

  const numpadDigit = /^Numpad([0-9])$/.exec(event.code);
  if (numpadDigit) return `Num${numpadDigit[1]}`;

  const codeKeys: Record<string, string> = {
    Space: "Space",
    NumpadEnter: "Enter",
    NumpadMultiply: "Multiply",
    NumpadAdd: "Add",
    NumpadComma: "Separator",
    NumpadSubtract: "Subtract",
    NumpadDecimal: "Decimal",
    NumpadDivide: "Divide",
    Semicolon: "Semicolon",
    Equal: "Equal",
    Comma: "Comma",
    Minus: "Minus",
    Period: "Period",
    Slash: "Slash",
    Backquote: "Backtick",
    BracketLeft: "LeftBracket",
    Backslash: "Backslash",
    BracketRight: "RightBracket",
    Quote: "Quote"
  };
  if (codeKeys[event.code]) return codeKeys[event.code];

  const eventKeys: Record<string, string> = {
    " ": "Space",
    Spacebar: "Space",
    ArrowLeft: "Left",
    ArrowUp: "Up",
    ArrowRight: "Right",
    ArrowDown: "Down",
    PrintScreen: "PrintScreen",
    ContextMenu: "Apps",
    Standby: "Sleep",
    BrowserBack: "BrowserBack",
    BrowserForward: "BrowserForward",
    BrowserRefresh: "BrowserRefresh",
    BrowserStop: "BrowserStop",
    BrowserSearch: "BrowserSearch",
    BrowserFavorites: "BrowserFavorites",
    BrowserHome: "BrowserHome",
    AudioVolumeMute: "VolumeMute",
    AudioVolumeDown: "VolumeDown",
    AudioVolumeUp: "VolumeUp",
    MediaTrackNext: "MediaNext",
    MediaTrackPrevious: "MediaPrevious",
    MediaStop: "MediaStop",
    MediaPlayPause: "MediaPlayPause",
    LaunchMail: "LaunchMail",
    LaunchMediaPlayer: "LaunchMediaSelect",
    LaunchApplication1: "LaunchApp1",
    LaunchApplication2: "LaunchApp2"
  };
  if (eventKeys[event.key]) return eventKeys[event.key];
  return canonicalKeyName(event.key);
}
