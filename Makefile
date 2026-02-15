.PHONY: build test spec gen update

build:
	mkdir -p bin
	go build -o bin/uis ./cmd/uis

test:
	go test ./...

spec:
	go run ./tools/uis-scrape --out internal/spec/data/api_spec.json

gen:
	go run ./tools/uis-gen --in internal/spec/data/api_spec.json --out internal/cli/generated_resources.go

update: spec gen build
