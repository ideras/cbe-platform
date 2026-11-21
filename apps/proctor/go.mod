module exam-platform/apps/proctor

go 1.26.8

replace exam-platform/shared/clientwire => ../../shared/clientwire

replace exam-platform/shared/examdoc => ../../shared/examdoc

require (
	exam-platform/shared/calc v0.0.0-00010101000000-000000000000
	exam-platform/shared/clientwire v0.0.0
	exam-platform/shared/examdoc v0.0.0
	github.com/wailsapp/wails/v3 v3.1.0
	golang.org/x/sys v0.48.0
	golang.org/x/text v0.42.0
)

require (
	exam-platform/shared/containerfile v0.0.0-00010101000000-000000000000 // indirect
	exam-platform/shared/examimage v0.0.0-00010101000000-000000000000 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	golang.org/x/term v0.46.0 // indirect
)

replace exam-platform/shared/calc => ../../shared/calc

replace exam-platform/shared/containerfile => ../../shared/containerfile

replace exam-platform/shared/examimage => ../../shared/examimage
