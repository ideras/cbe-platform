module exam-platform/apps/student

go 1.26.8

replace exam-platform/shared/clientwire => ../../shared/clientwire

require (
	exam-platform/shared/calc v0.0.0-00010101000000-000000000000
	exam-platform/shared/clientwire v0.0.0
	github.com/coder/websocket v1.8.14
	github.com/itchyny/volume-go v0.2.3
	github.com/wailsapp/wails/v3 v3.1.0
	golang.org/x/sys v0.48.0
	modernc.org/sqlite v1.60.1
	webtyp.com/screenshot v0.0.2
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/moutend/go-wca v0.3.0 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/term v0.46.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace exam-platform/shared/calc => ../../shared/calc
