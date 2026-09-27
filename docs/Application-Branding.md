# Application branding contract

PCController accepts product branding as build input. A branded build does not
require editing tracked source or generated assets. Pealayer can consume the
same `application-brand/v1` document so a bundled release uses one product
name, publisher, executable name, and icon set.

## Shared document

Paths are resolved relative to the JSON document. Unknown additive fields may
be retained by coordinators, but each product must validate every field it
uses. PCController currently consumes:

```json
{
  "format": "application-brand/v1",
  "applicationName": "Workshop Console",
  "tagline": "One workshop. Every controller.",
  "productName": "Workshop Control Suite",
  "companyName": "Example Devices LLC",
  "fileDescription": "Workshop controller host",
  "legalCopyright": "Copyright 2026 Example Devices LLC",
  "executableName": "workshop-host",
  "toastIcon": "assets/workshop-toast.png",
  "windowsIcons": {
    "APP": "assets/workshop.ico",
    "TRAY_CONNECTED": "assets/workshop-connected.ico",
    "TRAY_OFFLINE": "assets/workshop-offline.ico"
  }
}
```

`executableName` is extension-free; the Windows build appends `.exe`. It must
be a safe file name rather than a path. `windowsIcons` keys are Win32 resource
names. `APP` controls the executable/window icon; additional named resources
remain available to runtime UI code. Each ICO is structurally validated and
may carry multiple resolutions and bit depths. The packaged toast image is a
validated PNG.

## Build interface and precedence

Use `build.cmd --host-only --branding path\to\brand.json`. Focused switches can
override a shared document:

- `--app-name`, `--tagline`, `--product-name`, `--company-name`
- `--file-description`, `--copyright`, `--executable-name`
- `--icon FILE.ico` for `APP`
- repeatable `--resource-icon NAME=FILE.ico`
- `--toast-icon FILE.png`

The corresponding `PCCONTROLLER_BUILD_*` environment variables are supported
for automated packaging. Explicit switches win over the shared document; the
document wins over environment values; repository metadata and the official
resource template are final defaults.

The build records the effective identity, executable name, icon SHA-256 values,
and ICO sizes in `host-manifest.json`. Windows package inventory discovers the
declared executable instead of assuming `controller.exe`, then verifies the PE
ProductName, ProductVersion, OriginalFilename, source hash, and build time.
Official unbranded builds retain `controller.exe` and the existing canonical
PCController identity.

## Pealayer coordination

Pealayer should treat this document as build-time presentation input, not as a
runtime capability catalog. It should consume the common application/product,
publisher, copyright, and icon fields while keeping its own executable name in
a product-specific extension if one bundle needs distinct binaries. A bundle
coordinator should resolve the shared document once, apply intentional
per-application overrides, and publish both effective identities in its package
manifest.
