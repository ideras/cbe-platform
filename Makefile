export GO111MODULE := on
WAILS := go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29

.PHONY: test vet server examc student proctor desktop desktop-test script-test student-client-test student-ui-test proctor-client-test proctor-ui-test demo-setup demo-server demo-student demo-proctor

test:
	go test -race ./...
	cd shared/clientwire && go test -race ./...
	cd shared/examdoc && go test -race ./...

vet:
	go vet ./...
	cd shared/clientwire && go vet ./...
	cd shared/examdoc && go vet ./...

student-client-test: server student
	cd apps/student && EXAM_SERVER_BINARY="$(CURDIR)/build/cbe-portal-server" go test -race ./internal/client

student-ui-test:
	cd apps/student/frontend && npm ci && npm test && npm run test:ui

server:
	mkdir -p build
	CGO_ENABLED=0 go build -trimpath -o build/cbe-portal-server ./cmd/server

# examc checks an exam written in Markdown: build/examc exam.md (or -json to print what the proctor would import, or
# -fill answers.json to print the exam with those answers filled in).
examc:
	mkdir -p build
	cd shared/examdoc && CGO_ENABLED=0 go build -trimpath -o ../../build/examc ./cmd/examc

student:
	cd apps/student && $(WAILS) build

proctor:
	cd apps/proctor && $(WAILS) build

proctor-client-test: server proctor
	cd apps/proctor && EXAM_SERVER_BINARY="$(CURDIR)/build/cbe-portal-server" go test -race ./internal/dashboard

proctor-ui-test:
	cd apps/proctor/frontend && npm ci && npm test && npm run test:ui

desktop: student proctor

desktop-test: desktop
	cd apps/student && go test -race ./...
	cd apps/proctor && go test -race ./...
