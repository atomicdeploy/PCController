// Canonical user-visible product metadata shared by project-owned Node tools.
// Stable protocol, artifact, repository, and environment identifiers remain separate.

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
export const PRODUCT_METADATA_PATH = resolve(here, '..', 'Controller', 'web', 'package.json')

const requiredFields = [
	'version',
	'productName',
	'productShortName',
	'productTagline',
	'description',
	'productAppId',
	'productProtocol',
	'productConfigDirectory'
]

export function loadProductMetadata(path = PRODUCT_METADATA_PATH) {
	const metadata = JSON.parse(readFileSync(path, 'utf8'))
	for (const field of requiredFields) {
		if (typeof metadata[field] !== 'string' || !metadata[field].trim()) {
			throw new Error(`canonical product metadata ${field} must be a non-empty string`)
		}
	}
	return Object.freeze(metadata)
}

export const PRODUCT_METADATA = loadProductMetadata()

function environmentValue(environment, wanted) {
	for (const [name, value] of Object.entries(environment || {})) {
		if (name.toLowerCase() === wanted.toLowerCase()) return String(value ?? '').trim()
	}
	return ''
}

// Resolve the process-visible title while keeping stable technical IDs unchanged.
export function resolveProductTitle(environment = process.env, metadata = PRODUCT_METADATA) {
	const value = environmentValue(environment, 'APP_TITLE')
	if (value) return value
	const configured = String(metadata.productName || metadata.displayName || '').trim()
	if (configured) return configured
	return String(metadata.name || 'Controller').replace(/^@[^/]+\//, '').replace(/[-_]+/g, ' ')
}
