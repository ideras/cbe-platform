export GO111MODULE := on
# Architecture of the Windows cross-build: amd64 or arm64.
WINDOWS_ARCH ?= amd64
WAILS := go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.29

.PHONY: test vet server examc student student-windows proctor desktop desktop-test script-test student-client-test student-ui-test proctor-client-test proctor-ui-test demo-setup demo-server demo-student demo-proctor

test:
	go test -race ./...
	cd shared/clientwire && go test -race ./...
	cd shared/examdoc && go test -race ./...
	cd shared/examimage && go test -race ./...

vet:
	go vet ./...
	cd shared/clientwire && go vet ./...
	cd shared/examdoc && go vet ./...
	cd shared/examimage && go vet ./...

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

# student-windows cross-builds build/cbe-portal-student.exe from a Linux or macOS host (WINDOWS_ARCH=arm64 for ARM64). The
# Windows desktop links no CGO webview, so no Windows toolchain is needed; the binary is unsigned and has never run on Windows
# until it is tried there (see docs/deployment.md). The frontend and bindings are platform-neutral and come from the host;
# the bindings step analyses the host's own webview code, so it needs the host's cgo (GTK/WebKit) packages, like `make student`.
student-windows:
	mkdir -p build
	cd apps/student && $(WAILS) generate bindings -b -f '-tags bindings' ./cmd/student
	cd apps/student && npm --prefix frontend ci && npm --prefix frontend run build
	cd apps/student && CGO_ENABLED=0 GOOS=windows GOARCH=$(WINDOWS_ARCH) go build -trimpath -tags production -o ../../build/cbe-portal-student.exe ./cmd/student

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
