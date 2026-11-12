.PHONY: ui build test check run floor corpus

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
	scripts/check-floor.sh

floor:
	scripts/check-floor.sh

corpus:
	go test -tags corpus -run TestCorpus -v .

run: ui
	go run . -root .
