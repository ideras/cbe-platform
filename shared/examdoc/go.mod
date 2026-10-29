module exam-platform/shared/examdoc

go 1.26.8

require (
	exam-platform/shared/calc v0.0.0-00010101000000-000000000000
	exam-platform/shared/examimage v0.0.0-00010101000000-000000000000
	github.com/yuin/goldmark v1.8.2
)

replace exam-platform/shared/calc => ../calc

replace exam-platform/shared/examimage => ../examimage
