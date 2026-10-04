GO ?= go

.PHONY: fmt test vet check race tidy protocol-test simulate storage-test transport-test auth-test session-auth-test

fmt:
	@test -z "$$($(GO)fmt -l .)" || (echo "Go files are not formatted"; exit 1)

test:
	$(GO) test -timeout=5m ./...

race:
	$(GO) test -race -shuffle=on -timeout=5m ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

protocol-test:
	$(GO) test -race -shuffle=on ./internal/protocol ./internal/application/idempotency

simulate:
	$(GO) test -race -shuffle=on ./internal/simulation -run TestRunOneHundredAgents -count=1

storage-test:
	$(GO) test -race -shuffle=on -timeout=5m ./internal/storage/sqlite -count=1

transport-test:
	$(GO) test -race -shuffle=on -timeout=5m ./internal/transport/... -count=1

auth-test:
	$(GO) test -race -shuffle=on ./internal/auth -count=1

session-auth-test:
	$(GO) test -race -shuffle=on ./internal/application/sessionauth -count=1

check: fmt race vet
