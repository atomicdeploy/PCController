// Reviewed compatibility hardening for the locked xterm packages. The upstream
// async-work scheduler uses window messages but does not authenticate their
// sender. Apply this during both development and production dependency loading,
// rather than hand-editing a generated bundle or mutating node_modules.
export function guardXtermScheduler(code: string): string {
  const handler = /([A-Za-z_$][\w$]*)\.addEventListener\("message",\(?([A-Za-z_$][\w$]*)=>\{(?=if\(\2\.data&&\2\.data\.vscodeScheduleAsyncWork\))/g;
  let count = 0;
  const guarded = code.replace(handler, (prefix, _globalName: string, event: string) => {
    count++;
    return `${prefix}if(${event}.source!==globalThis||${event}.origin!==globalThis.location.origin)return;`;
  });
  if (count !== 1) throw new Error(`Review xterm scheduler origin guard: expected one handler, found ${count}`);
  return guarded;
}

export function xtermSchedulerGuard() {
  return {
    name: "pccontroller-xterm-scheduler-origin",
    enforce: "pre" as const,
    transform(code: string, id: string) {
      if (!/\/node_modules\/@xterm\/(?:xterm|addon-webgl)\/lib\/(?:xterm|addon-webgl)\.(?:mjs|js)$/.test(id.replaceAll("\\", "/"))) return;
      return { code: guardXtermScheduler(code), map: null };
    }
  };
}
