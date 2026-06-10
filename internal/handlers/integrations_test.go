package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"iag-erp/backend/internal/config"
	"iag-erp/backend/internal/events"
)

func TestIntegrationStatus_eventBus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bus := events.New(events.Config{Enabled: false})
	api := &API{
		Cfg: &config.Config{ServiceName: "erp"},
		Bus: bus,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/integrations/status", nil)
	api.IntegrationStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["kafka_topic"] != events.TopicOperations {
		t.Fatalf("kafka_topic = %v", body["kafka_topic"])
	}
	types, ok := body["event_types"].([]any)
	if !ok || len(types) == 0 {
		t.Fatalf("event_types = %#v", body["event_types"])
	}
}

func TestProductionOrderWebhook_badJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := &API{Cfg: &config.Config{}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{bad"))
	c.Request.Header.Set("Content-Type", "application/json")
	api.ProductionOrderWebhook(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}
