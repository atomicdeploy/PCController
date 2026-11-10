// Pure offline resolver, embedded verbatim in the classic browser script so
// file:// copies work without module requests, fetches or relaxed CSP.
export function createSchemaResolver(spec, rpcContract = spec) {
  const lookup = (ref) => {
    const external = ref.startsWith("./jsonrpc.schema.json#/");
    if (!external && !ref.startsWith("#/")) return undefined;
    const pointer = external ? ref.slice("./jsonrpc.schema.json".length) : ref;
    return pointer.slice(2).split("/").reduce(
      (value, key) => value?.[key.replaceAll("~1", "/").replaceAll("~0", "~")],
      external || pointer.startsWith("#/$defs/") ? rpcContract : spec,
    );
  };
  const resolve = (value) => {
    let current = value;
    const seen = new Set();
    while (current?.$ref && !seen.has(current.$ref)) {
      seen.add(current.$ref);
      const target = lookup(current.$ref);
      if (target === undefined) break;
      current = target;
    }
    return current;
  };
  const expand = (value, ancestors = new Set()) => {
    if (Array.isArray(value)) return value.map(item => expand(item, ancestors));
    if (!value || typeof value !== "object") return value;
    if (typeof value.$ref === "string" && !ancestors.has(value.$ref)) {
      const target = lookup(value.$ref);
      if (target !== undefined) {
        const next = new Set(ancestors).add(value.$ref);
        if (typeof target !== "object" || target === null) return target;
        const { $ref, ...siblings } = value;
        return expand({ ...target, ...siblings }, next);
      }
    }
    // Retain unknown refs and genuine cycle edges, but expand other siblings.
    return Object.fromEntries(Object.entries(value).map(([key, child]) =>
      [key, expand(child, ancestors)]));
  };
  return { resolve, expand };
}
