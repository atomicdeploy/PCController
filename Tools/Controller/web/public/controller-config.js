/* Optional portable WebUI transport configuration.
 *
 * Keep this file free of credentials. An extracted bundle may set
 * controller_origin to an HTTP(S) or WS(S) service root. Public HTTPS/WSS
 * targets must also appear exactly in trusted_controller_origins.
 */
globalThis.PCControllerWebConfig ??= {
  controller_origin: '',
  trusted_controller_origins: [],
}
