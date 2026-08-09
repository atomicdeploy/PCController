/// <reference types="vitest/config" />

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = fileURLToPath(new URL('.', import.meta.url))
const sourceManifest = JSON.parse(readFileSync(new URL('./public/manifest.webmanifest', import.meta.url), 'utf8')) as Record<string, unknown>
const packageMetadata = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')) as {
	version: string
  productName: string
  productShortName: string
  productTagline: string
  productFirstRunTagline: string
  productProtocol: string
  description: string
}

function metadata(value: unknown, field: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`package.json.${field} must be a non-empty string`)
  return value.trim()
}

function presentation(value: unknown, field: string, maximum: number): string {
  const text = metadata(value, field)
  if ([...text].length > maximum || [...text].some(character => /\p{Cc}/u.test(character))) {
    throw new Error(`${field} must be 1..${maximum} printable characters`)
  }
  return text
}

export function escapeProductHTML(value: string): string {
	return value
		.replaceAll('&', '&amp;')
		.replaceAll('<', '&lt;')
		.replaceAll('>', '&gt;')
		.replaceAll('"', '&quot;')
		.replaceAll("'", '&#39;')
}

export default defineConfig(() => {
  const productName = presentation(process.env.PCCONTROLLER_BUILD_APP_NAME || process.env.APP_TITLE || packageMetadata.productName, 'productName', 64)
  const productShortName = metadata(packageMetadata.productShortName, 'productShortName')
  const productTagline = presentation(process.env.PCCONTROLLER_BUILD_TAGLINE || process.env.APP_TAGLINE || packageMetadata.productFirstRunTagline, 'productFirstRunTagline', 96)
  const productProtocol = metadata(packageMetadata.productProtocol, 'productProtocol').toLowerCase()
  if (!/^[a-z][a-z0-9+.-]*$/.test(productProtocol)) throw new Error('package.json.productProtocol must be a valid URI scheme')
  const productDescription = metadata(packageMetadata.description, 'description')
	const hostVersion = metadata(packageMetadata.version, 'version')
	const hostBuildTime = String(process.env.PCCONTROLLER_HOST_BUILD_TIME || 'unknown').trim() || 'unknown'
	const productManifest = `${JSON.stringify({
		...sourceManifest,
		name: productName,
		short_name: productName,
		description: productDescription,
	}, null, 2)}\n`
  return {
  plugins: [{
    name: 'product-identity',
    transformIndexHtml: (html) => html
      .replaceAll('%PRODUCT_NAME%', escapeProductHTML(productName))
      .replaceAll('%PRODUCT_DESCRIPTION%', escapeProductHTML(productDescription)),
		configureServer(server) {
			server.middlewares.use((request, response, next) => {
				if (request.url?.split('?', 1)[0] !== '/manifest.webmanifest') return next()
				response.statusCode = 200
				response.setHeader('Content-Type', 'application/manifest+json; charset=utf-8')
				response.end(productManifest)
			})
		},
		generateBundle() {
			this.emitFile({ type: 'asset', fileName: 'manifest.webmanifest', source: productManifest })
		},
  }, react()],
  define: {
    __PRODUCT_NAME__: JSON.stringify(productName),
    __PRODUCT_SHORT_NAME__: JSON.stringify(productShortName),
    __PRODUCT_TAGLINE__: JSON.stringify(productTagline),
    __PRODUCT_PROTOCOL__: JSON.stringify(productProtocol),
		__HOST_VERSION__: JSON.stringify(hostVersion),
		__HOST_BUILD_TIME__: JSON.stringify(hostBuildTime),
  },
  base: '/',
  build: {
    outDir: resolve(webRoot, '../internal/webui/dist'),
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2022',
    assetsInlineLimit: 4096,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app-[hash].js',
        chunkFileNames: 'assets/chunk-[hash].js',
        assetFileNames: ({ names }) => {
          const name = names?.[0] ?? ''
          return name.endsWith('.woff2') ? 'fonts/[name]-[hash][extname]' : 'assets/[name]-[hash][extname]'
        }
      }
    }
  },
  server: {
    port: 4177,
    strictPort: true,
    proxy: {
      '/api': 'http://127.0.0.1:8787',
      '/ipc': { target: 'ws://127.0.0.1:8787', ws: true }
    }
  },
  test: {
    // Keep the heavy SSR import graph isolated without starting enough concurrent
    // workers to make Windows thread teardown miss Vitest's diagnostic deadline.
    pool: 'threads',
    maxWorkers: 4,
  }
  }
})
