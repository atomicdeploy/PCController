import { existsSync, readFileSync } from 'node:fs'
import { dirname, isAbsolute, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const keyPattern = /^[A-Za-z_][A-Za-z0-9_]*$/u

function unquote(value, source, line) {
  if (!value.startsWith('"') && !value.startsWith("'")) return value.replace(/\s+#.*$/u, '').trimEnd()
  const quote = value[0]
  let output = ''
  let escaped = false
  for (let index = 1; index < value.length; index++) {
    const character = value[index]
    if (quote === '"' && escaped) {
      output += ({ n: '\n', r: '\r', t: '\t', '"': '"', '\\': '\\' }[character] ?? character)
      escaped = false
      continue
    }
    if (quote === '"' && character === '\\') {
      escaped = true
      continue
    }
    if (character === quote) {
      const suffix = value.slice(index + 1).trim()
      if (suffix && !suffix.startsWith('#')) throw new Error(`${source}:${line}: unexpected content after quoted value`)
      return output
    }
    output += character
  }
  throw new Error(`${source}:${line}: unterminated quoted value`)
}

export function parseEnvFile(content, source = '.env') {
  const values = new Map()
  for (const [index, raw] of content.replace(/^\uFEFF/u, '').split(/\r?\n/u).entries()) {
    const line = index + 1
    const text = raw.trim()
    if (!text || text.startsWith('#')) continue
    const assignment = text.replace(/^export\s+/u, '')
    const separator = assignment.indexOf('=')
    if (separator <= 0) throw new Error(`${source}:${line}: expected KEY=VALUE`)
    const key = assignment.slice(0, separator).trim()
    if (!keyPattern.test(key)) throw new Error(`${source}:${line}: invalid environment variable name`)
    values.set(key, unquote(assignment.slice(separator + 1).trimStart(), source, line))
  }
  return values
}

export function resolveProjectEnvFile(environment = process.env, { root = repositoryRoot, cwd = process.cwd() } = {}) {
  const explicit = String(environment.PCCONTROLLER_ENV_FILE ?? '').trim()
  if (explicit) return isAbsolute(explicit) ? explicit : resolve(cwd, explicit)
  return join(root, '.env')
}

// Loads one project file without executing shell syntax or overriding inherited
// values. This keeps CI/service-manager settings authoritative and makes the
// same `.env` contract available to build, firmware, dependency, and audit
// entrypoints.
export function loadProjectEnv(environment = process.env, options = {}) {
  const path = resolveProjectEnvFile(environment, options)
  if (!existsSync(path)) {
    if (String(environment.PCCONTROLLER_ENV_FILE ?? '').trim()) {
      throw new Error(`explicit environment file does not exist: ${path}`)
    }
    return { path, loaded: false, applied: [] }
  }
  const values = parseEnvFile(readFileSync(path, 'utf8'), path)
  const applied = []
  for (const [key, value] of values) {
    if (Object.hasOwn(environment, key)) continue
    environment[key] = value
    applied.push(key)
  }
  // Children may start in another working directory. Preserve the selected
  // file itself, not the caller-relative spelling that found it.
  environment.PCCONTROLLER_ENV_FILE = path
  return { path, loaded: true, applied }
}

export { repositoryRoot }
