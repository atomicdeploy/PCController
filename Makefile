# Thin developer convenience facade. The project-owned Node build plan and
# VirtualBoard CMake presets remain the only build-policy owners.

ifeq ($(OS),Windows_NT)
BUILD = cmd.exe /d /c call build.cmd
else
BUILD = ./build.sh
endif

.DEFAULT_GOAL := help
.PHONY: help all host firmware virtual-board virtual-board-debug virtual-board-release virtual-board-relwithdebinfo plan clean-generated

help:
	@$(BUILD) --help

all:
	@$(BUILD) --all $(ARGS)

host:
	@$(BUILD) --host-only $(ARGS)

firmware:
	@$(BUILD) --firmware-only $(ARGS)

virtual-board:
	@$(BUILD) --virtual-board-only $(ARGS)

virtual-board-debug:
	@$(BUILD) --virtual-board-only --virtual-board-preset debug $(ARGS)

virtual-board-release:
	@$(BUILD) --virtual-board-only --virtual-board-preset release $(ARGS)

virtual-board-relwithdebinfo:
	@$(BUILD) --virtual-board-only --virtual-board-preset relwithdebinfo $(ARGS)

plan:
	@$(BUILD) --dry-run --plan-json $(ARGS)

clean-generated:
	@$(BUILD) --clean $(ARGS)
