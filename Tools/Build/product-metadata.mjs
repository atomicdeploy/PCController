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

const tuiConsoleIntegerFields = [
	['productTUIConsoleColumns', 56, 300],
	['productTUIConsoleRows', 18, 120],
	['productTUIConsoleFontSize', 5, 72]
]

export function validateProductMetadata(metadata) {
	for (const field of requiredFields) {
		if (typeof metadata[field] !== 'string' || !metadata[field].trim()) {
			throw new Error(`canonical product metadata ${field} must be a non-empty string`)
		}
	}
	if (typeof metadata.productTUIConsoleEnabled !== 'boolean') {
		throw new Error('canonical product metadata productTUIConsoleEnabled must be a boolean')
	}
	const fontFace = typeof metadata.productTUIConsoleFontFace === 'string'
		? metadata.productTUIConsoleFontFace.trim()
		: ''
	const printableFontFace = [...fontFace].every((character) => {
		const codePoint = character.codePointAt(0)
		return codePoint >= 0x20 && codePoint !== 0x7f
	})
	if (!fontFace || fontFace.length > 31 || !printableFontFace) {
		throw new Error('canonical product metadata productTUIConsoleFontFace must be 1..31 printable UTF-16 code units')
	}
	for (const [field, minimum, maximum] of tuiConsoleIntegerFields) {
		if (!Number.isInteger(metadata[field]) || metadata[field] < minimum || metadata[field] > maximum) {
			throw new Error(`canonical product metadata ${field} must be an integer from ${minimum} through ${maximum}`)
		}
	}
	return Object.freeze(metadata)
}

export function loadProductMetadata(path = PRODUCT_METADATA_PATH) {
	return validateProductMetadata(JSON.parse(readFileSync(path, 'utf8')))
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
