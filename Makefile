.PHONY: build test spec gen update

build:
	mkdir -p bin
	go build -o bin/uis ./cmd/uis

test:
	go test ./...
	python3 -m unittest discover -s tests -v

.PHONY: check skill-bundle install-hooks
check: test
	go vet ./...
	python3 scripts/skill_bundle.py

skill-bundle:
	python3 scripts/skill_bundle.py --output dist/uis-cli-skill

install-hooks:
	git config core.hooksPath .githooks

spec:
	go run ./tools/uis-scrape --out internal/spec/data/api_spec.json

gen:
	go run ./tools/uis-gen --in internal/spec/data/api_spec.json --out internal/cli/generated_resources.go

update: spec gen build

# Explicit opt-in and credentials are required; never part of `make test`.
.PHONY: test-production
test-production:
	go test -tags production ./e2e -count=1 -v -timeout 20m
