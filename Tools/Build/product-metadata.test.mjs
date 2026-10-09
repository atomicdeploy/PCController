import assert from 'node:assert/strict'
import test from 'node:test'

import {
	PRODUCT_METADATA,
	PRODUCT_METADATA_PATH,
	loadProductMetadata,
	resolveProductTitle,
	validateProductMetadata
} from './product-metadata.mjs'

test('canonical product metadata is loaded once from the web package', () => {
	assert.match(PRODUCT_METADATA_PATH.replaceAll('\\', '/'), /Tools\/Controller\/web\/package\.json$/)
	assert.equal(loadProductMetadata().productName, PRODUCT_METADATA.productName)
	for (const field of [
		'version',
		'productName', 'productShortName', 'productTagline', 'productFirstRunTagline', 'description',
		'productAppId', 'productProtocol', 'productConfigDirectory'
	]) assert.ok(PRODUCT_METADATA[field].trim(), `${field} must be populated`)
	assert.equal(typeof PRODUCT_METADATA.productTUIConsoleEnabled, 'boolean')
	assert.equal(typeof PRODUCT_METADATA.productTUIConsoleFontFace, 'string')
	for (const field of [
		'productTUIConsoleColumns', 'productTUIConsoleRows', 'productTUIConsoleFontSize'
	]) assert.ok(Number.isInteger(PRODUCT_METADATA[field]), `${field} must be an integer`)
})

test('TUI console font metadata matches the runtime safety contract', () => {
	for (const fontFace of ['x'.repeat(32), 'Con\nsolas']) {
		assert.throws(
			() => validateProductMetadata({
				...PRODUCT_METADATA,
				productTUIConsoleFontFace: fontFace
			}),
			/1\.\.31 printable UTF-16 code units/
		)
	}
})

test('build presentation metadata matches runtime length and text bounds', () => {
	for (const [field, value, expression] of [
		['productName', 'x'.repeat(65), /1\.\.64 printable characters/],
		['productFirstRunTagline', 'line\nbreak', /1\.\.96 printable characters/]
	]) {
		assert.throws(
			() => validateProductMetadata({ ...PRODUCT_METADATA, [field]: value }),
			expression
		)
	}
})

test('APP_TITLE overrides the canonical product name', () => {
	assert.equal(resolveProductTitle({
		APP_TITLE: 'Operator Console'
	}), 'Operator Console')
	assert.equal(resolveProductTitle({ app_title: 'Case-insensitive title' }), 'Case-insensitive title')
	assert.equal(resolveProductTitle({}, { productName: 'Configured Controller' }), 'Configured Controller')
})
