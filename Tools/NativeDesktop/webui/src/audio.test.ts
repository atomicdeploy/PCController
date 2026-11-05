import { describe, expect, it, vi } from "vitest";
import {
  BrowserAudioPlayer,
  bundledAudioURL,
  inspectWAV,
  normalizeImportedAudio,
  audioImportContentType,
  sha256Hex
} from "./audio";
import type { NotificationsConfig } from "./contracts";

describe("browser audio assets", () => {
  it("normalizes compressed imports once without playing or changing existing WAV bytes", async () => {
    const decode = vi.fn(async () => ({
      sampleRate: 8000, numberOfChannels: 1, length: 3,
      getChannelData: () => new Float32Array([-1, 0, 1])
    }) as unknown as AudioBuffer);
    for (const magic of ["ID3", "fLaC"]) {
      const result = await normalizeImportedAudio(new TextEncoder().encode(magic).buffer, decode);
      expect(inspectWAV(result).dataBytes).toBe(6);
      const view = new DataView(result);
      expect([view.getInt16(44, true), view.getInt16(46, true), view.getInt16(48, true)])
        .toEqual([-32768, 0, 32767]);
    }
    expect(decode).toHaveBeenCalledTimes(2);
    const wave = pcmWAV({});
    expect(await normalizeImportedAudio(wave, decode)).toBe(wave);
    expect(decode).toHaveBeenCalledTimes(2);
  });

  it("rejects overlong, invalid and unsupported compressed imports", async () => {
    const source = new TextEncoder().encode("fLaC").buffer;
    const long = vi.fn(async () => ({ sampleRate: 8000, numberOfChannels: 1, length: 88000 }) as AudioBuffer);
    await expect(normalizeImportedAudio(source, long)).rejects.toThrow(/10 seconds/);
    await expect(normalizeImportedAudio(source, async () => { throw new Error("decode failed"); }))
      .rejects.toThrow(/desktop host/);
    await expect(normalizeImportedAudio(new ArrayBuffer(40), long)).rejects.toThrow(/Choose PCM WAV/);
    await expect(normalizeImportedAudio(new ArrayBuffer((4 << 20) + 1), long)).rejects.toThrow(/4 MiB/);
    expect(long).toHaveBeenCalledOnce();
  });

  it("uploads the correct compressed media type including files with no browser MIME", () => {
    for (const [extension, mime] of [["WAV", "audio/wav"], ["mp3", "audio/mpeg"],
      ["flac", "audio/flac"]]) {
      expect(audioImportContentType(new File([], `chime.${extension}`))).toBe(mime);
    }
  });

  it("validates bounded uncompressed PCM WAV metadata", () => {
    const value = pcmWAV({ sampleRate: 8_000, frames: 800 });

    expect(inspectWAV(value)).toEqual({
      channels: 1,
      sampleRate: 8_000,
      bitsPerSample: 16,
      dataBytes: 1_600,
      durationMs: 100
    });
  });

  it("rejects disguised, compressed, and truncated audio", () => {
    expect(() => inspectWAV(new ArrayBuffer(44))).toThrow(/RIFF\/WAVE/);
    expect(() => inspectWAV(pcmWAV({ format: 3 }))).toThrow(/PCM WAV/);

    const truncated = pcmWAV({});
    new DataView(truncated).setUint32(40, 100_000, true);
    expect(() => inspectWAV(truncated)).toThrow(/truncated/);
  });

  it("resolves bundled cues relative to either /app/ or a static export", () => {
    expect(
      bundledAudioURL("message", "https://desk.example/app/")
    ).toBe("https://desk.example/app/shared/sfx/message.wav");
    expect(bundledAudioURL("on", "file:///C:/PCControllerDesktop/")).toBe(
      "file:///C:/PCControllerDesktop/shared/sfx/on.wav"
    );
  });

  it("retains a standards-compatible SHA-256 fallback for HTTP LAN origins", () => {
    expect(sha256Hex(new TextEncoder().encode("abc"))).toBe(
      "ba7816bf8f01cfea414140de5dae2223" +
        "b00361a396177a9cb410ff61f20015ad"
    );
  });

  it("decodes once and schedules bounded repeated cues without overlap", async () => {
    const starts: number[] = [];
    const decodeAudioData = vi.fn(async () => ({
      duration: 0.1
    }));
    const context = {
      state: "running",
      currentTime: 2,
      destination: {},
      resume: vi.fn(async () => undefined),
      decodeAudioData,
      createBufferSource: () => ({
        buffer: null,
        connect: vi.fn(),
        start: (at: number) => starts.push(at)
      })
    } as unknown as AudioContext;
    const fetcher = vi.fn(async () =>
      new Response(pcmWAV({}), {
        status: 200,
        headers: { "Content-Type": "audio/wav" }
      })
    );
    const player = new BrowserAudioPlayer({
      fetch: fetcher as typeof fetch,
      contextFactory: () => context,
      baseURL: () => "https://desk.example/app/"
    });

    await player.play("message", bundledSettings(), 2);
    await player.play("message", bundledSettings(), 1);

    expect(fetcher).toHaveBeenCalledOnce();
    expect(decodeAudioData).toHaveBeenCalledOnce();
    expect(starts).toHaveLength(3);
    expect(starts[0]).toBe(2);
    expect(starts[1]).toBeCloseTo(2.22);
    expect(starts[2]).toBeCloseTo(2.44);
  });

  it("falls back per cue when the selected custom browser asset is missing", async () => {
    const starts: number[] = [];
    const decodeAudioData = vi.fn(async () => ({ duration: 0.1 }));
    const context = {
      state: "running",
      currentTime: 0,
      destination: {},
      resume: vi.fn(async () => undefined),
      decodeAudioData,
      createBufferSource: () => ({
        buffer: null,
        connect: vi.fn(),
        start: (at: number) => starts.push(at)
      })
    } as unknown as AudioContext;
    const requested: string[] = [];
    const fetcher = vi.fn(async (input: RequestInfo | URL) => {
      requested.push(String(input));
      return new Response(pcmWAV({}), {
        status: 200,
        headers: { "Content-Type": "audio/wav" }
      });
    });
    const player = new BrowserAudioPlayer({
      fetch: fetcher as typeof fetch,
      contextFactory: () => context,
      baseURL: () => "https://desk.example/app/"
    });
    const custom = {
      ...bundledSettings(),
      sound_source: "custom" as const,
      sound_file: "a".repeat(64),
      sound_name: "Evicted.wav"
    };

    await player.play("on", custom);
    await player.play("off", custom);

    expect(requested).toEqual([
      "https://desk.example/app/shared/sfx/on.wav",
      "https://desk.example/app/shared/sfx/off.wav"
    ]);
    expect(decodeAudioData).toHaveBeenCalledTimes(2);
    expect(starts).toHaveLength(2);
  });

  it("keeps an autoplay-blocked cue queued until the next user gesture", async () => {
    let state: AudioContextState = "suspended";
    let resumeAttempts = 0;
    const start = vi.fn();
    const context = {
      get state() {
        return state;
      },
      currentTime: 0,
      destination: {},
      resume: vi.fn(async () => {
        resumeAttempts++;
        if (resumeAttempts === 1) throw new DOMException("blocked");
        state = "running";
      }),
      decodeAudioData: vi.fn(async () => ({ duration: 0.1 })),
      createBufferSource: () => ({
        buffer: null,
        connect: vi.fn(),
        start
      })
    } as unknown as AudioContext;
    const player = new BrowserAudioPlayer({
      fetch: vi.fn(async () => new Response(pcmWAV({}))) as typeof fetch,
      contextFactory: () => context,
      baseURL: () => "https://desk.example/app/"
    });
    const disarm = player.armUserGestureUnlock(document);

    const playback = player.play("message", bundledSettings());
    await Promise.resolve();
    expect(start).not.toHaveBeenCalled();
    document.dispatchEvent(new Event("pointerdown"));
    await playback;

    expect(start).toHaveBeenCalledOnce();
    disarm();
  });
});

function bundledSettings(): NotificationsConfig {
  return {
    enabled: false,
    state_changes: false,
    initial_sync: false,
    custom_message: false,
    sound: true,
    sound_source: "bundled",
    sound_file: "",
    sound_name: "",
    delivery: "auto"
  };
}

function pcmWAV({
  format = 1,
  channels = 1,
  sampleRate = 8_000,
  bitsPerSample = 16,
  frames = 80
}: {
  format?: number;
  channels?: number;
  sampleRate?: number;
  bitsPerSample?: number;
  frames?: number;
}): ArrayBuffer {
  const blockAlign = (channels * bitsPerSample) / 8;
  const dataBytes = frames * blockAlign;
  const data = new ArrayBuffer(44 + dataBytes);
  const bytes = new Uint8Array(data);
  const view = new DataView(data);
  writeFourCC(bytes, 0, "RIFF");
  view.setUint32(4, data.byteLength - 8, true);
  writeFourCC(bytes, 8, "WAVE");
  writeFourCC(bytes, 12, "fmt ");
  view.setUint32(16, 16, true);
  view.setUint16(20, format, true);
  view.setUint16(22, channels, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * blockAlign, true);
  view.setUint16(32, blockAlign, true);
  view.setUint16(34, bitsPerSample, true);
  writeFourCC(bytes, 36, "data");
  view.setUint32(40, dataBytes, true);
  return data;
}

function writeFourCC(
  bytes: Uint8Array,
  offset: number,
  value: string
): void {
  for (let index = 0; index < 4; index++) {
    bytes[offset + index] = value.charCodeAt(index);
  }
}
