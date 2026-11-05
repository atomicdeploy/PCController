import type { AudioCue, NotificationsConfig } from "./contracts";

const audioDatabaseName = "pccontroller-desktop.web.audio.v1";
const audioStoreName = "sounds";
const maxAudioBytes = 4 << 20;
const maxAudioSeconds = 10;
const beepIntervalSeconds = 0.22;

export interface WAVInfo {
  channels: number;
  sampleRate: number;
  bitsPerSample: number;
  dataBytes: number;
  durationMs: number;
}

export interface StoredAudioAsset extends WAVInfo {
  id: string;
  name: string;
  bytes: number;
  importedAt: string;
}

export interface BrowserAudioPlayback {
  play(
    cue: AudioCue,
    settings: NotificationsConfig,
    count?: number
  ): Promise<void>;
  importCustomAudio(file: File): Promise<StoredAudioAsset>;
  describe(id: string): Promise<StoredAudioAsset | null>;
  armUserGestureUnlock(target?: Document): () => void;
}

interface StoredAudioRecord extends StoredAudioAsset {
  data: ArrayBuffer;
}

interface BrowserAudioPlayerOptions {
  fetch?: typeof fetch;
  contextFactory?: () => AudioContext;
  baseURL?: () => string;
}

/**
 * One browser-side audio pipeline is shared by the direct dashboard, settings
 * preview, and future PWA surfaces. Semantic cue names keep callers decoupled
 * from physical files, while custom WAVs are de-duplicated by SHA-256 in IDB.
 */
export class BrowserAudioPlayer implements BrowserAudioPlayback {
  private readonly fetcher: typeof fetch;
  private readonly contextFactory: () => AudioContext;
  private readonly baseURL: () => string;
  private context: AudioContext | null = null;
  private readonly decoded = new Map<string, Promise<AudioBuffer>>();
  private playbackTail: Promise<void> = Promise.resolve();
  private nextStartTime = 0;
  private readonly unlockWaiters = new Set<() => void>();

  constructor(options: BrowserAudioPlayerOptions = {}) {
    this.fetcher =
      options.fetch ??
      ((input, init) => globalThis.fetch(input, init));
    this.contextFactory =
      options.contextFactory ?? createBrowserAudioContext;
    this.baseURL =
      options.baseURL ??
      (() => document.baseURI);
  }

  play(
    cue: AudioCue,
    settings: NotificationsConfig,
    count = 1
  ): Promise<void> {
    const normalizedCount = normalizeCueCount(count);
    // Resume synchronously while a settings-button click still carries browser
    // user activation. The queued decode/fetch may happen after that gesture.
    const context = this.audioContext();
    const resumed = this.ensureRunning(context);
    const operation = this.playbackTail
      .catch(() => undefined)
      .then(async () => {
        await resumed;
        const { key, decoded } = await this.resolveDecodedAsset(
          context,
          cue,
          settings
        );
        let buffer: AudioBuffer;
        try {
          buffer = await decoded;
        } catch (error) {
          this.decoded.delete(key);
          throw error;
        }
        const spacing = Math.max(
          beepIntervalSeconds,
          buffer.duration + 0.03
        );
        const firstStart = Math.max(
          context.currentTime,
          this.nextStartTime
        );
        for (let index = 0; index < normalizedCount; index++) {
          const source = context.createBufferSource();
          source.buffer = buffer;
          source.connect(context.destination);
          source.start(firstStart + index * spacing);
        }
        this.nextStartTime = firstStart + normalizedCount * spacing;
      });
    this.playbackTail = operation;
    return operation;
  }

  async importCustomAudio(file: File): Promise<StoredAudioAsset> {
    if (file.size > maxAudioBytes) {
      throw new Error("Custom audio must be 4 MiB or smaller.");
    }
    const source = await file.arrayBuffer();
    const data = await normalizeImportedAudio(source, (bytes) =>
      this.audioContext().decodeAudioData(bytes)
    );
    const info = inspectWAV(data);
    const id = await audioContentID(data);
    const importedAt = new Date().toISOString();
    const record: StoredAudioRecord = {
      id,
      name: safeDisplayName(file.name),
      bytes: data.byteLength,
      importedAt,
      ...info,
      data
    };
    await putStoredAudio(record);
    this.decoded.delete(`custom:${id}`);
    return withoutData(record);
  }

  async describe(id: string): Promise<StoredAudioAsset | null> {
    if (!validAssetID(id)) return null;
    try {
      const record = await getStoredAudio(id);
      if (!record || (await audioContentID(record.data)) !== id) return null;
      inspectWAV(record.data);
      return withoutData(record);
    } catch {
      return null;
    }
  }

  armUserGestureUnlock(target: Document = document): () => void {
    const unlock = () => {
      let context: AudioContext;
      try {
        context = this.audioContext();
      } catch {
        return;
      }
      if (context.state === "running") {
        for (const resolve of this.unlockWaiters) resolve();
        this.unlockWaiters.clear();
        return;
      }
      void context
        .resume()
        .then(() => {
          if (context.state !== "running") return;
          for (const resolve of this.unlockWaiters) resolve();
          this.unlockWaiters.clear();
        })
        .catch(() => undefined);
    };
    target.addEventListener("pointerdown", unlock, {
      capture: true,
      passive: true
    });
    target.addEventListener("keydown", unlock, { capture: true });
    return () => {
      target.removeEventListener("pointerdown", unlock, true);
      target.removeEventListener("keydown", unlock, true);
    };
  }

  private audioContext(): AudioContext {
    if (!this.context || this.context.state === "closed") {
      this.context = this.contextFactory();
    }
    return this.context;
  }

  private async ensureRunning(context: AudioContext): Promise<void> {
    if (audioContextRunning(context)) return;
    try {
      await context.resume();
    } catch {
      // Browser autoplay policy may require a later user gesture.
    }
    if (audioContextRunning(context)) return;
    await new Promise<void>((resolve) => {
      this.unlockWaiters.add(resolve);
    });
  }

  private async resolveDecodedAsset(
    context: AudioContext,
    cue: AudioCue,
    settings: NotificationsConfig
  ): Promise<{ key: string; decoded: Promise<AudioBuffer> }> {
    if (
      settings.sound_source === "custom" &&
      validAssetID(settings.sound_file)
    ) {
      const customKey = `custom:${settings.sound_file}`;
      const cached = this.decoded.get(customKey);
      if (cached) {
        return { key: customKey, decoded: cached };
      }
      try {
        const record = await getStoredAudio(settings.sound_file);
        if (
          record &&
          (await audioContentID(record.data)) === settings.sound_file
        ) {
          inspectWAV(record.data);
          const decoded = context.decodeAudioData(record.data.slice(0));
          this.decoded.set(customKey, decoded);
          return { key: customKey, decoded };
        }
      } catch {
        // A missing, corrupt, or unavailable browser asset must not make every
        // future event silent. DirectDeviceAPI repairs the persisted selection;
        // this last-resort path still plays the cue-specific bundled effect.
      }
    }
    return this.resolveBundledAsset(context, cue);
  }

  private resolveBundledAsset(
    context: AudioContext,
    cue: AudioCue
  ): { key: string; decoded: Promise<AudioBuffer> } {
    const key = `bundled:${cue}`;
    const cached = this.decoded.get(key);
    if (cached) return { key, decoded: cached };
    const decoded = (async () => {
      const response = await this.fetcher(bundledAudioURL(cue, this.baseURL()), {
        credentials: "same-origin",
        cache: "default"
      });
      if (!response.ok) {
        throw new Error(
          `Bundled ${cue} sound is unavailable (HTTP ${response.status}).`
        );
      }
      const data = await response.arrayBuffer();
      inspectWAV(data);
      return context.decodeAudioData(data.slice(0));
    })();
    this.decoded.set(key, decoded);
    return { key, decoded };
  }
}

export const browserAudio = new BrowserAudioPlayer();

export function bundledAudioURL(cue: AudioCue, baseURL: string): string {
  return new URL(`shared/sfx/${cue}.wav`, baseURL).href;
}

export const audioImportAccept = ".wav,.mp3,.flac,audio/wav,audio/mpeg,audio/flac";

export function audioImportContentType(file: File): string {
  const extension = file.name.split(".").pop()?.toLowerCase();
  const types: Record<string, string> = {
    wav: "audio/wav", mp3: "audio/mpeg", flac: "audio/flac"
  };
  return types[extension || ""] || file.type || "application/octet-stream";
}

// Direct/browser-only mode has no host decoder. Decode with Web Audio once,
// without resuming the context or connecting a source to the speakers. Persist
// the same bounded PCM format used by the native host, not compressed bytes.
export async function normalizeImportedAudio(
  data: ArrayBuffer,
  decode: (data: ArrayBuffer) => Promise<AudioBuffer>
): Promise<ArrayBuffer> {
  if (data.byteLength > maxAudioBytes) {
    throw new Error("Custom audio must be 4 MiB or smaller.");
  }
  const bytes = new Uint8Array(data);
  if (fourCC(bytes, 0) === "RIFF") {
    inspectWAV(data);
    return data;
  }
  if (!(fourCC(bytes, 0) === "fLaC" ||
    (bytes[0] === 73 && bytes[1] === 68 && bytes[2] === 51) ||
    (bytes[0] === 255 && (bytes[1] & 0xe0) === 0xe0))) {
    throw new Error("Choose PCM WAV, MP3 or FLAC audio.");
  }
  let buffer: AudioBuffer;
  try {
    buffer = await decode(data.slice(0));
  } catch {
    throw new Error("This browser cannot decode this audio file. Try importing it through the desktop host.");
  }
  const { sampleRate, numberOfChannels: channels, length: frames } = buffer;
  if (sampleRate < 8000 || sampleRate > 96000 || channels < 1 || channels > 2 ||
    frames < 1 || frames > sampleRate * maxAudioSeconds) {
    throw new Error("Custom audio must be mono or stereo, 8–96 kHz and no longer than 10 seconds.");
  }
  const size = frames * channels * 2;
  if (size + 44 > maxAudioBytes) {
    throw new Error("Decoded custom audio must be 4 MiB or smaller.");
  }
  const wave = new ArrayBuffer(44 + size);
  const out = new Uint8Array(wave);
  const view = new DataView(wave);
  const tag = (offset: number, text: string) => {
    for (let i = 0; i < text.length; i++) out[offset + i] = text.charCodeAt(i);
  };
  tag(0, "RIFF"); tag(8, "WAVEfmt "); tag(36, "data");
  view.setUint32(4, wave.byteLength - 8, true);
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, channels, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * channels * 2, true);
  view.setUint16(32, channels * 2, true);
  view.setUint16(34, 16, true);
  view.setUint32(40, size, true);
  for (let channel = 0; channel < channels; channel++) {
    const values = buffer.getChannelData(channel);
    for (let i = 0; i < frames; i++) {
      if (!Number.isFinite(values[i])) throw new Error("Invalid audio sample.");
      const value = Math.max(-1, Math.min(1, values[i]));
      view.setInt16(44 + (i * channels + channel) * 2,
        Math.round(value * (value < 0 ? 32768 : 32767)), true);
    }
  }
  inspectWAV(wave);
  return wave;
}

export function inspectWAV(data: ArrayBuffer): WAVInfo {
  if (data.byteLength < 44) {
    throw new Error("Custom audio is too small to be a WAV file.");
  }
  if (data.byteLength > maxAudioBytes) {
    throw new Error("Custom audio must be 4 MiB or smaller.");
  }
  const bytes = new Uint8Array(data);
  const view = new DataView(data);
  if (fourCC(bytes, 0) !== "RIFF" || fourCC(bytes, 8) !== "WAVE") {
    throw new Error("Custom audio must use the RIFF/WAVE container.");
  }
  if (view.getUint32(4, true) + 8 !== data.byteLength) {
    throw new Error("Custom audio has an inconsistent RIFF size.");
  }

  let formatOffset = -1;
  let formatBytes = 0;
  let dataBytes = 0;
  let dataSeen = false;
  for (let offset = 12; offset < bytes.length; ) {
    if (offset + 8 > bytes.length) {
      throw new Error("Custom audio contains a truncated WAV chunk header.");
    }
    const kind = fourCC(bytes, offset);
    const size = view.getUint32(offset + 4, true);
    const body = offset + 8;
    const end = body + size;
    const paddedEnd = end + (size & 1);
    if (end > bytes.length || paddedEnd > bytes.length) {
      throw new Error("Custom audio contains a truncated WAV chunk.");
    }
    if (kind === "fmt ") {
      if (formatOffset >= 0) {
        throw new Error("Custom audio contains duplicate fmt chunks.");
      }
      formatOffset = body;
      formatBytes = size;
    } else if (kind === "data") {
      if (dataSeen) {
        throw new Error("Custom audio contains duplicate data chunks.");
      }
      dataSeen = true;
      dataBytes = size;
    }
    offset = paddedEnd;
  }
  if (formatOffset < 0 || formatBytes < 16 || dataBytes < 1) {
    throw new Error("Custom audio needs valid fmt and data chunks.");
  }

  const format = view.getUint16(formatOffset, true);
  if (format !== 1) {
    throw new Error("Custom audio must be uncompressed PCM WAV.");
  }
  const channels = view.getUint16(formatOffset + 2, true);
  const sampleRate = view.getUint32(formatOffset + 4, true);
  const byteRate = view.getUint32(formatOffset + 8, true);
  const blockAlign = view.getUint16(formatOffset + 12, true);
  const bitsPerSample = view.getUint16(formatOffset + 14, true);
  if (channels < 1 || channels > 2) {
    throw new Error("Custom audio must be mono or stereo.");
  }
  if (sampleRate < 8_000 || sampleRate > 96_000) {
    throw new Error(
      "Custom audio sample rate must be between 8 kHz and 96 kHz."
    );
  }
  if (![8, 16, 24, 32].includes(bitsPerSample)) {
    throw new Error("Custom audio must use 8, 16, 24, or 32-bit PCM.");
  }
  const expectedAlign = (channels * bitsPerSample) / 8;
  if (
    blockAlign !== expectedAlign ||
    byteRate !== sampleRate * expectedAlign
  ) {
    throw new Error("Custom audio has inconsistent PCM format metadata.");
  }
  if (dataBytes % blockAlign !== 0) {
    throw new Error("Custom audio PCM data is not block-aligned.");
  }
  const frames = dataBytes / blockAlign;
  if (frames > sampleRate * maxAudioSeconds) {
    throw new Error(
      `Custom audio must be no longer than ${maxAudioSeconds} seconds.`
    );
  }
  const durationMs = Math.max(
    1,
    Math.round((frames / sampleRate) * 1_000)
  );
  return {
    channels,
    sampleRate,
    bitsPerSample,
    dataBytes,
    durationMs
  };
}

function createBrowserAudioContext(): AudioContext {
  const AudioContextConstructor = (
    globalThis as typeof globalThis & {
      webkitAudioContext?: typeof AudioContext;
    }
  ).AudioContext ?? (
    globalThis as typeof globalThis & {
      webkitAudioContext?: typeof AudioContext;
    }
  ).webkitAudioContext;
  if (!AudioContextConstructor) {
    throw new Error("This browser does not support Web Audio playback.");
  }
  return new AudioContextConstructor();
}

function audioContextRunning(context: AudioContext): boolean {
  return context.state === "running";
}

function normalizeCueCount(value: number): number {
  if (!Number.isInteger(value) || value < 1) {
    throw new Error("Audio cue count must be a positive integer.");
  }
  return Math.min(value, 10);
}

function fourCC(bytes: Uint8Array, offset: number): string {
  return String.fromCharCode(
    bytes[offset],
    bytes[offset + 1],
    bytes[offset + 2],
    bytes[offset + 3]
  );
}

function safeDisplayName(value: string): string {
  const leaf = value.split(/[\\/]/).at(-1)?.trim() ?? "";
  const safe = Array.from(leaf)
    .filter((character) => {
      const code = character.codePointAt(0) ?? 0;
      return code >= 0x20 && code !== 0x7f;
    })
    .join("")
    .slice(0, 128);
  return safe || "Custom sound.wav";
}

function validAssetID(value: string): boolean {
  return /^[a-f0-9]{64}$/.test(value);
}

export async function audioContentID(data: ArrayBuffer): Promise<string> {
  if (globalThis.crypto?.subtle) {
    const digest = await globalThis.crypto.subtle.digest("SHA-256", data);
    return hex(new Uint8Array(digest));
  }
  // Web Crypto is unavailable on some plain-HTTP LAN origins. Direct mode
  // still needs stable cross-tab identities there, so retain a bounded,
  // deterministic SHA-256 fallback for the at-most-4-MiB import.
  return sha256Hex(new Uint8Array(data));
}

export function sha256Hex(input: Uint8Array): string {
  const constants = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
    0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
    0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
    0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
    0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
    0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
  ];
  const paddedLength = Math.ceil((input.length + 9) / 64) * 64;
  const message = new Uint8Array(paddedLength);
  message.set(input);
  message[input.length] = 0x80;
  const view = new DataView(message.buffer);
  const bitLength = input.length * 8;
  view.setUint32(paddedLength - 8, Math.floor(bitLength / 0x1_0000_0000));
  view.setUint32(paddedLength - 4, bitLength >>> 0);

  const hash = new Uint32Array([
    0x6a09e667,
    0xbb67ae85,
    0x3c6ef372,
    0xa54ff53a,
    0x510e527f,
    0x9b05688c,
    0x1f83d9ab,
    0x5be0cd19
  ]);
  const words = new Uint32Array(64);
  for (let offset = 0; offset < message.length; offset += 64) {
    for (let index = 0; index < 16; index++) {
      words[index] = view.getUint32(offset + index * 4);
    }
    for (let index = 16; index < 64; index++) {
      const before15 = words[index - 15];
      const before2 = words[index - 2];
      const sigma0 =
        rotateRight(before15, 7) ^
        rotateRight(before15, 18) ^
        (before15 >>> 3);
      const sigma1 =
        rotateRight(before2, 17) ^
        rotateRight(before2, 19) ^
        (before2 >>> 10);
      words[index] =
        (words[index - 16] +
          sigma0 +
          words[index - 7] +
          sigma1) >>>
        0;
    }

    let [a, b, c, d, e, f, g, h] = hash;
    for (let index = 0; index < 64; index++) {
      const sum1 =
        rotateRight(e, 6) ^ rotateRight(e, 11) ^ rotateRight(e, 25);
      const choice = (e & f) ^ (~e & g);
      const temp1 =
        (h + sum1 + choice + constants[index] + words[index]) >>> 0;
      const sum0 =
        rotateRight(a, 2) ^ rotateRight(a, 13) ^ rotateRight(a, 22);
      const majority = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (sum0 + majority) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + temp1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temp1 + temp2) >>> 0;
    }
    hash[0] = (hash[0] + a) >>> 0;
    hash[1] = (hash[1] + b) >>> 0;
    hash[2] = (hash[2] + c) >>> 0;
    hash[3] = (hash[3] + d) >>> 0;
    hash[4] = (hash[4] + e) >>> 0;
    hash[5] = (hash[5] + f) >>> 0;
    hash[6] = (hash[6] + g) >>> 0;
    hash[7] = (hash[7] + h) >>> 0;
  }
  const output = new Uint8Array(32);
  const outputView = new DataView(output.buffer);
  for (let index = 0; index < hash.length; index++) {
    outputView.setUint32(index * 4, hash[index]);
  }
  return hex(output);
}

function rotateRight(value: number, amount: number): number {
  return (value >>> amount) | (value << (32 - amount));
}

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (value) =>
    value.toString(16).padStart(2, "0")
  ).join("");
}

function openAudioDatabase(): Promise<IDBDatabase> {
  if (typeof indexedDB === "undefined") {
    return Promise.reject(
      new Error("This browser cannot store a custom sound.")
    );
  }
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(audioDatabaseName, 1);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(audioStoreName)) {
        request.result.createObjectStore(audioStoreName, {
          keyPath: "id"
        });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () =>
      reject(
        request.error ??
          new Error("The custom audio database could not be opened.")
      );
  });
}

async function getStoredAudio(
  id: string
): Promise<StoredAudioRecord | null> {
  const database = await openAudioDatabase();
  try {
    const transaction = database.transaction(audioStoreName, "readonly");
    const request = transaction.objectStore(audioStoreName).get(id);
    return await new Promise((resolve, reject) => {
      request.onsuccess = () =>
        resolve((request.result as StoredAudioRecord | undefined) ?? null);
      request.onerror = () =>
        reject(
          request.error ??
            new Error("The custom sound could not be read.")
        );
    });
  } finally {
    database.close();
  }
}

async function putStoredAudio(record: StoredAudioRecord): Promise<void> {
  const database = await openAudioDatabase();
  try {
    const transaction = database.transaction(audioStoreName, "readwrite");
    transaction.objectStore(audioStoreName).put(record);
    await new Promise<void>((resolve, reject) => {
      transaction.oncomplete = () => resolve();
      transaction.onabort = () =>
        reject(
          transaction.error ??
            new Error("The custom sound import was cancelled.")
        );
      transaction.onerror = () =>
        reject(
          transaction.error ??
            new Error("The custom sound could not be stored.")
        );
    });
  } finally {
    database.close();
  }
}

function withoutData(record: StoredAudioRecord): StoredAudioAsset {
  const { data: _data, ...metadata } = record;
  return metadata;
}
