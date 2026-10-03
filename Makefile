# Development tasks, see DEVELOPING.md.

# keep in sync with .github/workflows/ci.yaml
GOLANGCI_LINT_VERSION := v2.13.2
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

BENCH ?= .
COUNT ?= 6
FUZZ ?= FuzzLiblzo2Differential
FUZZTIME ?= 2m

.PHONY: test test-liblzo2 fuzz bench golden lint lint-fix

test:
	go test -race ./...

test-liblzo2:
	go test -race -tags liblzo2 ./...

fuzz:
	go test -tags liblzo2 -run '^$$' -fuzz '^$(FUZZ)$$' -fuzztime $(FUZZTIME) .

bench:
	go test -tags liblzo2 -run '^$$' -bench '$(BENCH)' -count $(COUNT) .

golden:
	go test -tags liblzo2 -run '^TestLiblzo2UpdateGolden$$' -update-golden .

lint:
	$(GOLANGCI_LINT) run

lint-fix:
	$(GOLANGCI_LINT) fmt
	$(GOLANGCI_LINT) run --fix
