package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"

	"exam-platform/internal/service"
)

// catalogOnly answers the exam catalog and nothing else.
type catalogOnly struct{ PublishAPI }

func (catalogOnly) Catalog(context.Context) (service.ExamCatalog, error) {
	return service.ExamCatalog{Exams: []service.ExamEntry{}}, nil
}

// The exam catalog, which the dashboard reads on every poll, carries the server's release, so the dashboard can say which it
// is connected to and compare it with each student's application.
func TestExamCatalogReportsTheServerRelease(t *testing.T) {
	w := httptest.NewRecorder()
	examCatalogHandler(catalogOnly{}, "v0.16.0", slog.New(slog.DiscardHandler)).ServeHTTP(w, httptest.NewRequest("GET", "/admin/exams", nil))
	var body struct {
		Version string `json:"server_version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body.Version != "v0.16.0" {
		t.Fatalf("catalog = %d %s", w.Code, w.Body.String())
	}
}
