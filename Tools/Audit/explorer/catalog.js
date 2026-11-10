// No network requests, API mutations, credentials, CDN, or synthetic live data.
(() => {
  const root = document.querySelector(".catalog");
  const spec = JSON.parse(document.getElementById("catalog").textContent);
  const streams = root.dataset.mode === "streams";
  const $ = (id) => document.getElementById(id);
  const el = (tag, text, cls) => {
    const node = document.createElement(tag);
    if (text !== undefined) node.textContent = text;
    if (cls) node.className = cls;
    return node;
  };
  const resolve = (value) => {
    let current = value;
    const seen = new Set();
    while (current?.$ref?.startsWith("#/") && !seen.has(current.$ref)) {
      seen.add(current.$ref);
      current = current.$ref
        .slice(2)
        .split("/")
        .reduce(
          (v, k) => v?.[k.replaceAll("~1", "/").replaceAll("~0", "~")],
          spec,
        );
    }
    return current ?? value;
  };
  const sample = (schema, depth = 0) => {
    schema = resolve(schema);
    if (!schema || depth > 8) return {};
    if (schema.examples?.length) return schema.examples[0];
    if ("const" in schema) return schema.const;
    if ("default" in schema) return schema.default;
    if (schema.enum?.length) return schema.enum[0];
    if (schema.oneOf) return sample(schema.oneOf[0], depth + 1);
    if (schema.type === "object" || schema.properties)
      return Object.fromEntries(
        Object.entries(schema.properties ?? {})
          .filter(([key]) => !schema.required || schema.required.includes(key))
          .map(([key, value]) => [key, sample(value, depth + 1)]),
      );
    if (schema.type === "array") return [];
    if (schema.type === "boolean") return false;
    if (schema.type === "integer" || schema.type === "number")
      return schema.minimum ?? 0;
    return "<value>";
  };
  const items = streams
    ? Object.entries(spec.channels).flatMap(([channel, c]) =>
        Object.entries(c.messages ?? {}).map(([name, message]) => {
          const m = resolve(message);
          return {
            id: channel + "." + name,
            name: m.name ?? name,
            summary: m.summary ?? m.description ?? c.description ?? "",
            capability: channel,
            address: c.address,
            schema: resolve(m.payload),
            raw: m,
          };
        }),
      )
    : Object.entries(spec["x-methods"]).map(([name, m]) => ({
        id: name,
        name,
        summary: m.summary,
        capability: m.capability,
        safe: m.idempotency === "safe",
        schema: m.params_schema
          ? resolve(m.params_schema)
          : (m.request_fields ?? null),
        inferred: !m.params_schema,
        result: m.result_schema ? resolve(m.result_schema) : null,
        raw: m,
      }));
  const caps = $("capability");
  for (const cap of [...new Set(items.map((v) => v.capability))].sort()) {
    const option = el("option", cap.replaceAll("_", " "));
    option.value = cap;
    caps.append(option);
  }
  let selected = "";
  try {
    selected = decodeURIComponent(location.hash.slice(1));
  } catch {}
  function code(title, value) {
    const box = el("div", undefined, "codebox");
    const head = el("div", undefined, "codehead");
    head.append(el("span", title));
    const button = el("button", "Copy");
    button.type = "button";
    const text =
      typeof value === "string" ? value : JSON.stringify(value, null, 2);
    button.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(text);
        button.textContent = "Copied";
        setTimeout(() => (button.textContent = "Copy"), 1400);
      } catch {
        button.textContent = "Select text to copy";
      }
    });
    head.append(button);
    box.append(head, el("pre", text));
    return box;
  }
  function schemaPanel(title, value) {
    const details = el("details");
    details.append(el("summary", title));
    details.append(el("pre", JSON.stringify(value, null, 2)));
    return details;
  }
  function show(item) {
    selected = item.id;
    history.replaceState(null, "", "#" + encodeURIComponent(selected));
    const detail = $("detail");
    detail.replaceChildren();
    const badges = el("div", undefined, "badges");
    badges.append(
      el("span", streams ? "STREAM" : "JSON-RPC", "badge"),
      el("span", item.capability.replaceAll("_", " "), "badge"),
    );
    if (!streams)
      badges.append(
        el(
          "span",
          item.safe ? "READ / EVENTS" : "EXPLICIT ACTION",
          item.safe ? "badge safe" : "badge write",
        ),
      );
    detail.append(badges, el("h1", streams ? item.name : item.id));
    if (item.summary) detail.append(el("p", item.summary, "description"));
    if (streams) {
      detail.append(el("div", item.address, "endpoint"));
      if (item.schema?.examples?.length)
        detail.append(code("Message example", item.schema.examples[0]));
      detail.append(schemaPanel("Message schema", item.schema ?? item.raw));
      const examples = spec["x-socket-io"] ?? spec["x-socketio"];
      if (item.capability === "socketIO" && examples)
        detail.append(schemaPanel("Socket.IO framing", examples));
    } else {
      detail.append(
        el("div", "HTTP POST /api/rpc · WebSocket /ipc · NDJSON", "endpoint"),
      );
      const params = item.raw.request_template ?? (item.schema ? sample(item.schema) : {});
      const request = { jsonrpc: "2.0", id: 1, method: item.id, params };
      if (item.schema && item.inferred)
        detail.append(
          el(
            "p",
            "Field types from the handler; cross-field admission constraints still apply.",
            "note",
          ),
        );
      if (!item.schema && !item.safe)
        detail.append(
          el(
            "p",
            "This operation has a free-form parameter contract. The envelope below is not a complete actuation request.",
            "note",
          ),
        );
      detail.append(
        code(item.schema ? "Request template" : "JSON-RPC envelope", request),
      );
      if (item.schema)
        detail.append(schemaPanel("Parameter schema", item.schema));
      if (item.result) detail.append(schemaPanel("Result schema", item.result));
      detail.append(schemaPanel("Method contract", item.raw));
      if (
        item.safe &&
        [
          "controller.snapshot",
          "controller.status",
          "controller.ping",
        ].includes(item.id)
      ) {
        const origin = /^https?:$/.test(location.protocol)
          ? location.origin
          : "http://127.0.0.1:8787";
        detail.append(
          code(
            "Minimal cURL example",
            `curl '${origin}/api/rpc' -H 'Content-Type: application/json' --data '${JSON.stringify(request)}'`,
          ),
        );
      }
    }
    for (const button of $("operations").children)
      button.setAttribute(
        "aria-current",
        String(button.dataset.id === selected),
      );
  }
  function filter() {
    const query = $("search").value.toLowerCase().trim();
    const list = items.filter(
      (item) =>
        (!caps.value || item.capability === caps.value) &&
        (!query ||
          [item.id, item.name, item.summary, item.capability]
            .join(" ")
            .toLowerCase()
            .includes(query)),
    );
    $("count").textContent = list.length + " / " + items.length;
    const nav = $("operations");
    nav.replaceChildren();
    for (const item of list) {
      const button = el("button");
      button.type = "button";
      button.dataset.id = item.id;
      button.setAttribute("aria-current", String(selected === item.id));
      button.append(el("span", "", "dot"), el("span", item.id));
      button.addEventListener("click", () => show(item));
      nav.append(button);
    }
    if (!list.length) nav.append(el("div", "No matching operations", "empty"));
    if (!items.some((item) => item.id === selected) && list[0]) show(list[0]);
  }
  $("search").addEventListener("input", filter);
  caps.addEventListener("change", filter);
  document.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key === "k") {
      event.preventDefault();
      $("search").focus();
    }
  });
  filter();
  const initial = items.find((item) => item.id === selected);
  if (initial) show(initial);
})();
