package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type captureCounter struct {
	records [][]Label
}

func (c *captureCounter) Inc(_ context.Context, labels ...Label) {
	copied := make([]Label, len(labels))
	copy(copied, labels)
	c.records = append(c.records, copied)
}

func (c *captureCounter) Add(_ context.Context, _ float64, labels ...Label) {
	c.Inc(context.Background(), labels...)
}

type captureHistogram struct {
	records [][]Label
}

func (h *captureHistogram) Record(_ context.Context, _ float64, labels ...Label) {
	copied := make([]Label, len(labels))
	copy(copied, labels)
	h.records = append(h.records, copied)
}

func labelValue(labels []Label, key string) (string, bool) {
	for _, label := range labels {
		if label.Key == key {
			return label.Value, true
		}
	}
	return "", false
}

func TestGinHTTPMiddlewareUnknownRouteForUnmatchedPath(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	counter := &captureCounter{}
	histogram := &captureHistogram{}
	httpMetrics := &HTTPServerMetrics{
		service:      "svc",
		requestTotal: counter,
		duration:     histogram,
	}

	router := gin.New()
	router.Use(GinHTTPMiddleware(httpMetrics))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/random-scan-value", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if len(counter.records) != 1 {
		t.Fatalf("counter records = %d, want 1", len(counter.records))
	}

	route, ok := labelValue(counter.records[0], LabelRoute)
	if !ok {
		t.Fatalf("missing %q label", LabelRoute)
	}
	if route != UnknownRoute {
		t.Fatalf("route label = %q, want %q", route, UnknownRoute)
	}
}

func TestGinHTTPMiddlewareUsesRouteTemplate(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	counter := &captureCounter{}
	histogram := &captureHistogram{}
	httpMetrics := &HTTPServerMetrics{
		service:      "svc",
		requestTotal: counter,
		duration:     histogram,
	}

	router := gin.New()
	router.Use(GinHTTPMiddleware(httpMetrics))
	router.GET("/orders/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if len(counter.records) != 1 {
		t.Fatalf("counter records = %d, want 1", len(counter.records))
	}

	route, ok := labelValue(counter.records[0], LabelRoute)
	if !ok {
		t.Fatalf("missing %q label", LabelRoute)
	}
	if route != "/orders/:id" {
		t.Fatalf("route label = %q, want %q", route, "/orders/:id")
	}
}
