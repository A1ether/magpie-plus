# The recipes (clean's rm -rf too) name paths relative to the working
# folder, so they run only in magpie's own, known by its go.mod: run from
# another one (make -f path/to/Makefile) they would act there. make -C
# works. go.mod is read relative to the working folder, so a path with
# spaces (or a symlinked Makefile) changes nothing.
ifneq ($(shell sed -n 's/^module //p' go.mod 2>/dev/null),github.com/yetone/magpie)
  $(error run make in magpie's folder (make -C path/to/magpie), not in $(CURDIR))
endif

# A tag may hold $, ( or `, which the shell lines below would run: the
# version keeps only what a version is spelled with.
VERSION ?= $(or $(shell git describe --tags --always --dirty 2>/dev/null | tr -cd 'A-Za-z0-9._+-'),dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
TAGS     = production
TARGETS  = darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64 android/arm64

# The GUI links the platform webview through cgo, so it is built natively.
# `nogui` builds the terminal-only magpie, which cross-compiles anywhere.
# macOS 12 is the oldest Go (1.25 and later) runs on; Info.plist says so too.
ifeq ($(shell uname -s),Darwin)
  export CGO_CFLAGS  = -mmacosx-version-min=12.0
  export CGO_LDFLAGS = -mmacosx-version-min=12.0
endif
# On Linux, GTK 3 and WebKitGTK 4.1: older distributions have them, where
# Wails' default GTK 4 and WebKitGTK 6 would leave them out.
ifeq ($(shell uname -s),Linux)
  TAGS += gtk3
endif
# Termux uses Android's libc and has no desktop webview. Keep the web UI
# and terminal commands, without linking GTK or Wails.
ifeq ($(shell go env GOOS),android)
  TAGS = nogui
endif

.PHONY: build cli install test test-ui app install-app icons release release-cli release-windows release-linux clean dev dev-once balance-board power-meter-build preview update update-upstream update-power-meter upstream-archive upstream-archive-check

build:
	go build -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" -o magpie .

cli:
	CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o magpie .

install:
	go install -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" .

test:
	go vet -tags "$(TAGS)" ./... && go test -tags "$(TAGS)" ./...

# The Balance board's sidecar: power-meter's compiled server, installed where
# internal/powermeterproc looks for it (<config>/magpie/powermeter/server.js),
# its sibling modules beside it and public/ above it. The whole dist/ tree
# goes, because server.js imports its siblings by relative path. It is not
# bundled into magpie: power-meter is its own project, and a sidecar is one
# 'make balance-board' away (or MAGPIE_POWERMETER_SERVER_JS pointing at your
# own build). Its source is kept in this repository at third_party/power-meter
# (a git subtree: `make update-power-meter` follows it), and 'npm run build'
# writes dist/ there. A checkout elsewhere still works: set
# POWER_METER=<checkout>. A portable magpie keeps its files beside the app
# instead: set POWER_METER_DEST to that data folder.
POWER_METER ?= third_party/power-meter
POWER_METER_DEST ?= $(or $(XDG_CONFIG_HOME),$(HOME)/.config)/magpie/powermeter

# node_modules is not kept in this repository, so the first build installs it
# from package-lock.json. tsc writes dist/server.js.
power-meter-build:
	@test -f $(POWER_METER)/package.json || { echo "no power-meter at $(POWER_METER): set POWER_METER=<checkout>" >&2; exit 1; }
	@cd $(POWER_METER) && { test -d node_modules || npm ci; } && npm run build
	@test -f $(POWER_METER)/dist/server.js || { echo "$(POWER_METER)/dist/server.js is missing after the build" >&2; exit 1; }

balance-board:
	@test -d $(POWER_METER) || { echo "power-meter not at $(POWER_METER): set POWER_METER=<checkout>" >&2; exit 1; }
	@test -f $(POWER_METER)/dist/server.js || { echo "$(POWER_METER)/dist/server.js is missing: run 'npm run build' there first" >&2; exit 1; }
	@mkdir -p $(POWER_METER_DEST)
	@rsync -a --delete-excluded --exclude '*.map' --exclude 'power-meter-sidecar' $(POWER_METER)/dist/ $(POWER_METER_DEST)/
	@rsync -a $(POWER_METER)/public/ $(POWER_METER_DEST)/public/
	@echo "  balance board · sidecar installed in $(POWER_METER_DEST) · turn it on in the Balance board page"

# What to look at first, in a browser: both sources built, the sidecar
# installed, then 'magpie web' and its link (Ctrl-C stops it). Both ports are
# the preview's own. A running magpie holds 127.0.0.1:3425 and serves the
# gateway there; MAGPIE_ADDR moves this run's gateway to 3437, so the page is
# the build being looked at and not the other one's, and the older magpie
# keeps the agents' traffic. The desktop app is built only once this page is
# as it should be: make app.
PREVIEW_ADDR ?= 127.0.0.1:3436
PREVIEW_GATEWAY ?= 127.0.0.1:3437

preview: power-meter-build balance-board build
	@MAGPIE_ADDR=$(PREVIEW_GATEWAY) ./magpie web --addr $(PREVIEW_ADDR)

# Following the two upstreams. Both sources are in this repository: magpie's
# tree is the fork of yetone/magpie (whose branches and tags are archived
# under vendor/upstream/, see above), and power-meter's is the subtree at
# third_party/power-meter. Each stops on a conflict and leaves it to be
# resolved by hand. Then: make preview.
update:
	@git diff --quiet && git diff --cached --quiet || { echo "the working tree has changes: commit or stash them first" >&2; exit 1; }
	@$(MAKE) --no-print-directory update-power-meter
	@$(MAKE) --no-print-directory update-upstream
	@echo "  updated · next: make preview"

update-upstream:
	git fetch upstream
	git merge --no-edit upstream/main

update-power-meter:
	git fetch power-meter
	git subtree pull --prefix third_party/power-meter power-meter main --squash

# Keeps a copy of yetone/magpie's branches and tags in this repository, under
# vendor/upstream/, so the project survives upstream deleting its repository.
# The daily Upstream archive workflow runs the same script; --check reports
# what is missing without pushing. See FORK.md §14.
upstream-archive:
	build/upstream-archive.sh

upstream-archive-check:
	build/upstream-archive.sh --check

# The browser regressions under internal/gui/tests: the real pages in
# Playwright's Chromium and WebKit; see internal/gui/tests/README.md.
UI_TESTS = $(wildcard internal/gui/tests/*.test.cjs)
# Files are independent and run in parallel; 1 diagnoses a flaky one.
UI_TEST_CONCURRENCY ?= 2

test-ui:
	@test -n "$(UI_TESTS)" || { echo "no GUI tests found under internal/gui/tests" >&2; exit 1; }
	node --test --test-concurrency=$(UI_TEST_CONCURRENCY) $(UI_TESTS)

# macOS bundle: menu bar app with no Dock icon (LSUIElement). This fork's
# bundle is "magpie plus.app" - what the Finder shows, and what its
# Info.plist names it (internal/gui/desktop.go's appName is the same name
# for the window, the menu bar and the Mac's app menu). The program inside
# keeps the name magpie: the magpie:// links and the terminal's magpie run
# it.
APP = magpie plus.app

app: build
	@rm -rf "$(APP)"
	@mkdir -p "$(APP)/Contents/MacOS" "$(APP)/Contents/Resources"
	@cp magpie "$(APP)/Contents/MacOS/magpie"
	@cp build/darwin/magpie.icns "$(APP)/Contents/Resources/magpie.icns"
	@cp build/darwin/Assets.car "$(APP)/Contents/Resources/Assets.car"
	@cp THIRD_PARTY_NOTICES "$(APP)/Contents/Resources/THIRD_PARTY_NOTICES"
	@sed 's/@VERSION@/$(VERSION)/' build/darwin/Info.plist > "$(APP)/Contents/Info.plist"
	@echo "  $(APP)"

# Installs it where the Finder and Launchpad look, and leaves one copy: two
# bundles with one bundle id (the build folder's and this one) show "magpie
# plus" twice, and the one a click opens is whichever LaunchServices saw
# last. So the build's copy goes once the installed one is in place.
INSTALL_DIR ?= /Applications
LSREGISTER = /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister

install-app: app
	@rm -rf "$(INSTALL_DIR)/$(APP)"
	@ditto "$(APP)" "$(INSTALL_DIR)/$(APP)"
	@rm -rf "$(APP)"
	@$(LSREGISTER) -f "$(INSTALL_DIR)/$(APP)" >/dev/null 2>&1 || true
	@echo "  $(INSTALL_DIR)/$(APP) · $(VERSION) · the build folder's copy is gone, so there is one"

icons:
	@go run build/icon/gen.go tray internal/gui/tray.png
	@go run build/icon/gen.go tray-flap internal/gui/trayflap
	@go run build/icon/gen.go app 64 internal/gui/icon.png
	@go run build/icon/gen.go app 1024 internal/gui/icon-1024.png
	@go run build/icon/gen.go app 256 internal/gui/icon-256.png
	@rm -rf build/darwin/magpie.iconset && mkdir -p build/darwin/magpie.iconset
	@for s in 16 32 128 256 512; do \
		go run build/icon/gen.go app $$s build/darwin/magpie.iconset/icon_$${s}x$${s}.png; \
		go run build/icon/gen.go app $$((s*2)) build/darwin/magpie.iconset/icon_$${s}x$${s}@2x.png; \
	done
	@iconutil -c icns build/darwin/magpie.iconset -o build/darwin/magpie.icns && rm -rf build/darwin/magpie.iconset
	@# macOS 26 draws the Dock icon from AppIcon.icon, light and dark (Xcode 26's actool);
	@# the compiled Assets.car is kept in the tree so a release needn't have it
	@go run build/icon/gen.go glyph 1024 build/darwin/AppIcon.icon/Assets/magpie.png
	@rm -rf build/darwin/actool && mkdir -p build/darwin/actool
	@xcrun actool build/darwin/AppIcon.icon --compile build/darwin/actool --platform macosx \
		--minimum-deployment-target 11.0 --app-icon AppIcon --output-partial-info-plist build/darwin/actool/partial.plist >/dev/null
	@mv build/darwin/actool/Assets.car build/darwin/Assets.car && rm -rf build/darwin/actool

release: clean build
	@mkdir -p dist
	@cp magpie dist/magpie-$(shell go env GOOS)-$(shell go env GOARCH)
	@$(MAKE) --no-print-directory release-cli

release-cli:
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ $$os = windows ] && ext=.exe; \
		echo "  $$os/$$arch (cli)"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -tags nogui -trimpath -ldflags="$(LDFLAGS)" -o dist/magpie-cli-$$os-$$arch$$ext . || exit 1; \
	done

# The desktop app for Windows: the system WebView2 needs no cgo, so both
# arches cross-compile. Linked as a GUI program (no console window), with
# the icon, version and a DPI-aware manifest from build/windows. It carries
# magpie.com, the console program a terminal runs for `magpie` and waits
# for, which it writes beside itself (internal/update/com.go, #3737).
# The launcher is built without the commit stamped in (-buildvcs=false):
# magpie.com is written again whenever its bytes differ, and with the
# commit in them every release wrote a new one though the launcher was the
# same (TestLauncherSameAtEveryCommit).
WINVER = $(shell echo $(VERSION) | sed -E 's/^v//; s/[^0-9.].*//')
release-windows:
	@mkdir -p dist
	@go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json --arch amd64,arm64 --out rsrc \
		--file-version "$(or $(WINVER),0.0.0)" --product-version "$(or $(WINVER),0.0.0)"
	@for arch in amd64 arm64; do \
		echo "  windows/$$arch (app)"; \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch go build -trimpath -buildvcs=false -ldflags="-s -w" -o internal/update/launcher-$$arch.com ./internal/update/launcher || exit 1; \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch go build -tags production,comlauncher -trimpath -ldflags="$(LDFLAGS) -H windowsgui" -o dist/magpie-windows-$$arch.exe . || exit 1; \
	done; rm -f rsrc_windows_*.syso internal/update/launcher-*.com

# The desktop app for Linux links GTK 3 and WebKitGTK 4.1 through cgo, so it
# is built natively, one arch per machine.
release-linux:
	@mkdir -p dist
	@echo "  linux/$(shell go env GOARCH) (app)"
	@go build -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" -o dist/magpie-linux-$(shell go env GOARCH) .

clean:
	rm -rf magpie magpie.exe magpie.app "magpie plus.app" dist rsrc_windows_*.syso internal/update/launcher-*.com

# Development: the UI is served from internal/gui/assets and the window
# reloads itself when a file there is saved. With fswatch installed, the
# windows stay up and a Go change rebuilds and restarts only the backend
# behind them (build/dev.sh); without it, dev-once runs the app as one
# process. Uses its own ports so a running magpie keeps serving the agents.
DEV_ADDR ?= 127.0.0.1:3426
DEV_UI ?= 127.0.0.1:3427
DEV_BACKEND ?= 127.0.0.1:3428
DEV_CONTROL ?= 127.0.0.1:3429
dev:
	@if command -v fswatch >/dev/null; then \
	  MAGPIE_ADDR=$(DEV_ADDR) MAGPIE_DEV_UI=$(DEV_UI) MAGPIE_DEV_BACKEND=$(DEV_BACKEND) MAGPIE_DEV_CONTROL=$(DEV_CONTROL) build/dev.sh; \
	else $(MAKE) --no-print-directory dev-once; fi

dev-once:
	@go build -tags dev -o magpie-dev . && echo "  magpie-dev · UI from internal/gui/assets, reload on save · gateway $(DEV_ADDR)"
	@MAGPIE_ADDR=$(DEV_ADDR) MAGPIE_DEV_UI=$(DEV_UI) ./magpie-dev app
