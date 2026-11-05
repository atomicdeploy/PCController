export const coordinatorExactProxyPattern =
  "^/(?:status|reconnect|diagnostics|config|notifications|version|events|healthz)(?:\\?|$)";

export const coordinatorFamilyProxyPattern =
  "^/(?:device|window|terminal|ui|audio)(?:/|$)";
