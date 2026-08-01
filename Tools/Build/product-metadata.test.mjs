import assert from 'node:assert/strict'
import test from 'node:test'

import {
	PRODUCT_METADATA,
	PRODUCT_METADATA_PATH,
	loadProductMetadata,
	resolveProductTitle
} from './product-metadata.mjs'

test('canonical product metadata is loaded once from the web package', () => {
	assert.match(PRODUCT_METADATA_PATH.replaceAll('\\', '/'), /Tools\/Controller\/web\/package\.json$/)
	assert.equal(loadProductMetadata().productName, PRODUCT_METADATA.productName)
	for (const field of [
		'productName', 'productShortName', 'productTagline', 'description',
		'productAppId', 'productProtocol', 'productConfigDirectory'
	]) assert.ok(PRODUCT_METADATA[field].trim(), `${field} must be populated`)
})

test('APP_TITLE overrides the legacy title variable and canonical product name', () => {
	assert.equal(resolveProductTitle({
		PCCONTROLLER_APP_TITLE: 'Legacy title', APP_TITLE: 'Operator Console'
	}), 'Operator Console')
	assert.equal(resolveProductTitle({ app_title: 'Case-insensitive title' }), 'Case-insensitive title')
	assert.equal(resolveProductTitle({}, { productName: 'Configured Controller' }), 'Configured Controller')
})
