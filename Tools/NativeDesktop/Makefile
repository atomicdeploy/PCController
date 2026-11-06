ARCH ?= amd64
EDITION ?= $(strip $(shell tr -d '\r\n' < build-edition.txt))
BUILD_NUMBER ?= 0
FORCE ?= 0
COMPRESS ?= 0

.PHONY: windows-cross windows-cross-develop windows-cross-production windows-sdk-cross verify-windows

windows-cross:
	bash ./tools/build-linux.sh \
		--architecture "$(ARCH)" \
		--edition "$(EDITION)" \
		--build-number "$(BUILD_NUMBER)"$(if $(filter 1 true yes,$(FORCE)), --force)$(if $(filter 1 true yes,$(COMPRESS)), --compress)

windows-cross-develop:
	$(MAKE) windows-cross EDITION=develop

windows-cross-production:
	$(MAKE) windows-cross EDITION=production

windows-sdk-cross:
	bash ./tools/build-sdk-linux.sh \
		--architecture "$(ARCH)" \
		--build-number "$(BUILD_NUMBER)"$(if $(filter 1 true yes,$(FORCE)), --force)

verify-windows:
	go run ./tools/verify-pe \
		--file ./dist/PCControllerDesktop.exe \
		--arch "$(ARCH)" \
		--edition "$(EDITION)" \
		--build-number "$(BUILD_NUMBER)"
