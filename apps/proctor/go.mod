module exam-platform/apps/proctor

go 1.26.8

replace exam-platform/shared/clientwire => ../../shared/clientwire

replace exam-platform/shared/examdoc => ../../shared/examdoc

require (
	exam-platform/shared/calc v0.0.0-00010101000000-000000000000
	exam-platform/shared/clientwire v0.0.0
	exam-platform/shared/examdoc v0.0.0
	github.com/wailsapp/wails/v3 v3.0.0-beta.29
	golang.org/x/text v0.39.0
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	golang.org/x/sys v0.46.0 // indirect
)

replace exam-platform/shared/calc => ../../shared/calc
