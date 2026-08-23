# INFRA-CAP Makefile — self-contained: all deps live inside this folder

GO      := ./tools/go/bin/go
TW      := ./tools/tailwindcss

export GOMODCACHE := $(CURDIR)/.gomodcache
export GOPATH     := $(CURDIR)/.gopath

PKGS := ./cmd/... ./internal/...

.PHONY: run build test vet tidy tools css css-watch sync-templates

## run: dev server on :1112 (loads .env)
run: sync-templates
	@set -a && . ./.env && set +a && $(GO) run ./cmd/server

## build: compile server binary
build:
	$(GO) build -o infracap ./cmd/server

## test: run all tests
test:
	$(GO) test $(PKGS)

## vet
vet:
	$(GO) vet $(PKGS)

## tidy
tidy:
	$(GO) mod tidy

## tools: download toolchain + frontend libs into project folder (idempotent)
tools:
	@test -x $(GO) || (mkdir -p tools && cd tools && curl -fsSLo go.tgz https://go.dev/dl/go1.27.0.linux-amd64.tar.gz && tar -xzf go.tgz && rm go.tgz)
	@test -x $(TW) || curl -fsSLo $(TW) https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64 && chmod +x $(TW)
	@test -f tools/daisyui/package/daisyui.css || (curl -fsSLo /tmp/daisyui.tgz https://registry.npmjs.org/daisyui/-/daisyui-5.7.20.tgz && mkdir -p tools/daisyui && tar -xzf /tmp/daisyui.tgz -C tools/daisyui)
	@test -f web/static/js/htmx.min.js || curl -fsSLo web/static/js/htmx.min.js https://unpkg.com/htmx.org@2/dist/htmx.min.js
	@test -f web/static/js/alpine.min.js || curl -fsSLo web/static/js/alpine.min.js https://unpkg.com/alpinejs@3/dist/cdn.min.js

## css: build tailwind + daisyui css
css:
	$(TW) -i web/tailwind.css --content "web/templates/**/*.html" -o web/static/css/app.css

## css-watch: rebuild css on change (dev)
css-watch:
	$(TW) -i web/tailwind.css --content "web/templates/**/*.html" -o web/static/css/app.css --watch

## sync-templates: copy web/templates into the embed dir
sync-templates:
	@rm -rf internal/web/templates && cp -r web/templates internal/web/templates

## restart: kill running server, rebuild, start again in background
restart:
	@-pkill -f "exe/server" 2>/dev/null || true
	@-pkill -f "cmd/server" 2>/dev/null || true
	@sleep 0.6
	@make sync-templates >/dev/null
	@nohup bash -c 'set -a && . ./.env && set +a && exec $(GO) run -tags dev ./cmd/server' > /tmp/infracap.log 2>&1 & echo "server restarted -> http://localhost:1112 (log: /tmp/infracap.log)"

## watch: auto sync templates + css + restart on any template/go change (requires no deps)
watch:
	@bash tools/dev-watch.sh
