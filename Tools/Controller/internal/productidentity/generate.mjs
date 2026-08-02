import { readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const directory = fileURLToPath(new URL('.', import.meta.url))
const packagePath = fileURLToPath(new URL('../../web/package.json', import.meta.url))
const metadata = JSON.parse(await readFile(packagePath, 'utf8'))

const required = [
  'version',
  'productName', 'productShortName', 'productTagline', 'description',
  'productAppId', 'productProtocol', 'productConfigDirectory',
]
for (const field of required) {
  if (typeof metadata[field] !== 'string' || metadata[field].trim() === '') {
    throw new Error(`web/package.json.${field} must be a non-empty string`)
  }
}

const quote = (value) => JSON.stringify(value.trim())
const version = metadata.version.trim()
const numericVersion = version.match(/^(\d+)\.(\d+)\.(\d+)/)
if (!numericVersion) throw new Error('web/package.json.version must begin with major.minor.patch')
const fixedVersion = `${numericVersion[1]}.${numericVersion[2]}.${numericVersion[3]}.0`
const source = `// Code generated from ../../web/package.json by generate.mjs; DO NOT EDIT.

package productidentity

const (
\tVersion         = ${quote(version)}
\tDefaultTitle    = ${quote(metadata.productName)}
\tShortName       = ${quote(metadata.productShortName)}
\tTagline         = ${quote(metadata.productTagline)}
\tDescription     = ${quote(metadata.description)}
\tStableAppID     = ${quote(metadata.productAppId)}
\tProtocolScheme  = ${quote(metadata.productProtocol)}
\tConfigDirectory = ${quote(metadata.productConfigDirectory)}
)
`

const resourceURL = new URL('../../winres/winres.json', import.meta.url)
const resources = JSON.parse(await readFile(resourceURL, 'utf8'))
resources.RT_MANIFEST['#1']['0409'].description = `${metadata.productName.trim()} host utility`
const versionInfo = resources.RT_VERSION['#1']['0409'].info['0409']
const fixedVersionInfo = resources.RT_VERSION['#1']['0409'].fixed
versionInfo.FileDescription = `${metadata.productName.trim()} Host`
versionInfo.ProductName = metadata.productName.trim()
versionInfo.FileVersion = version
versionInfo.ProductVersion = version
fixedVersionInfo.file_version = fixedVersion
fixedVersionInfo.product_version = fixedVersion
const resourceSource = `${JSON.stringify(resources, null, 2)}\n`

const outputs = [
  [new URL('metadata_gen.go', import.meta.url), source],
  [resourceURL, resourceSource],
]
if (process.argv.includes('--check')) {
  const drift = []
  for (const [url, expected] of outputs) {
    const actual = await readFile(url, 'utf8')
    if (actual.replaceAll('\r\n', '\n') !== expected) drift.push(fileURLToPath(url))
  }
  if (drift.length) throw new Error(`generated product metadata is stale: ${drift.join(', ')}; run node ${fileURLToPath(import.meta.url)}`)
} else {
  for (const [url, content] of outputs) await writeFile(url, content, 'utf8')
}
