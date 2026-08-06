// Shared, secret-safe network retry policy for dependency subprocesses.

const proxyPattern = /^(?:HTTP|HTTPS|ALL|FTP|NO)_PROXY$/iu
const routeProxyPattern = /^(?:HTTP|HTTPS|ALL|FTP)_PROXY$/iu
const firmwareProxyPattern = /^ARDUINO_NETWORK_PROXY$/iu

function configuredProxyNames(environment = process.env) {
  return Object.keys(environment).filter((name) =>
    (proxyPattern.test(name) || firmwareProxyPattern.test(name)) &&
    String(environment[name] ?? '').trim(),
  ).sort((left, right) => left.localeCompare(right))
}

function hasConfiguredProxyRoute(environment = process.env) {
  return Object.keys(environment).some((name) =>
    (routeProxyPattern.test(name) || firmwareProxyPattern.test(name)) &&
    String(environment[name] ?? '').trim(),
  )
}

function environmentWithoutProxy(environment = process.env) {
  return Object.fromEntries(Object.entries(environment).filter(([name]) =>
    !proxyPattern.test(name) && !firmwareProxyPattern.test(name),
  ))
}

// Retry exactly once without proxy variables only after the configured route
// fails. The caller's environment is never mutated and secret values are not
// returned in diagnostics.
function withDirectFallback(operation, options = {}) {
  const environment = options.environment ?? process.env
  const directRetry = options.directRetry !== false
  try {
    return { value: operation(environment, false), usedDirectFallback: false }
  } catch (configuredError) {
    if (!directRetry || !hasConfiguredProxyRoute(environment)) throw configuredError
    try {
      return {
        value: operation(environmentWithoutProxy(environment), true),
        usedDirectFallback: true,
      }
    } catch (directError) {
      const error = new Error(`configured network route failed; one direct retry also failed: ${directError.message}`)
      error.cause = configuredError
      throw error
    }
  }
}

export {
  configuredProxyNames,
  environmentWithoutProxy,
  hasConfiguredProxyRoute,
  withDirectFallback,
}
