/// <reference types="vitest/config" />

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = fileURLToPath(new URL('.', import.meta.url))
const packageMetadata = JSON.parse(readFileSync(new URL('./package.json', import.meta.url), 'utf8')) as {
	version: string
  productName: string
  productShortName: string
  productTagline: string
  productProtocol: string
  description: string
}

function metadata(value: unknown, field: string): string {
  if (typeof value !== 'string' || value.trim() === '') throw new Error(`package.json.${field} must be a non-empty string`)
  return value.trim()
}

export default defineConfig(() => {
  const productName = metadata(process.env.APP_TITLE || packageMetadata.productName, 'productName')
  const productShortName = metadata(packageMetadata.productShortName, 'productShortName')
  const productTagline = metadata(packageMetadata.productTagline, 'productTagline')
  const productProtocol = metadata(packageMetadata.productProtocol, 'productProtocol').toLowerCase()
  if (!/^[a-z][a-z0-9+.-]*$/.test(productProtocol)) throw new Error('package.json.productProtocol must be a valid URI scheme')
  const productDescription = metadata(packageMetadata.description, 'description')
	const hostVersion = metadata(packageMetadata.version, 'version')
	const hostBuildTime = String(process.env.PCCONTROLLER_HOST_BUILD_TIME || 'unknown').trim() || 'unknown'
  return {
  plugins: [{
    name: 'product-identity',
    transformIndexHtml: (html) => html
      .replaceAll('%PRODUCT_NAME%', productName)
      .replaceAll('%PRODUCT_DESCRIPTION%', productDescription),
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
    // Pure Node/SSR tests do not need child processes; threads avoid intermittent
    // Windows fork termination stalls without weakening teardown diagnostics.
    pool: 'threads',
  }
  }
})
