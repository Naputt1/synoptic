.PHONY: ui build test check run

# The Go binary embeds ui/dist/index.html, so every Go target needs the UI built.
ui:
	cd ui && bun install && bun run build

build: ui
	go build ./...

test: ui
	pkgs=$$(go list ./... | grep -v '/node_modules/'); go test $$pkgs

check: ui
	cd ui && bun run typecheck && bun run lint
	pkgs=$$(go list ./... | grep -v '/node_modules/'); go vet $$pkgs && go test $$pkgs

run: ui
	go run . -root .
