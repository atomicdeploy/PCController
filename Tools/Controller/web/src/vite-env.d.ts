/// <reference types="vite/client" />

declare module '*.css'

declare const __PRODUCT_NAME__: string
declare const __PRODUCT_SHORT_NAME__: string
declare const __PRODUCT_TAGLINE__: string
declare const __PRODUCT_PROTOCOL__: string
declare const __HOST_VERSION__: string
declare const __HOST_BUILD_TIME__: string

interface PCControllerWebConfig {
  controller_origin?: string
  trusted_controller_origins?: string[]
}

interface Window {
  PCControllerWebConfig?: PCControllerWebConfig
}

declare var PCControllerWebConfig: PCControllerWebConfig | undefined
