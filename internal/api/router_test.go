package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterListsStepTypes(t *testing.T) {
	router := NewRouter(NewHandler(nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/step-types", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := false
	for _, def := range body {
		if def["id"] == "script.run" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected script.run in step type list, got %#v", body)
	}
}

func TestRouterValidatesTypedPipeline(t *testing.T) {
	router := NewRouter(NewHandler(nil))
	payload := []byte(`{
		"spec": {
			"version": "1.1",
			"name": "typed-api",
			"steps": [
				{
					"name": "test",
					"type": "script.run",
					"image": "golang:1.25",
					"with": { "script": "go test ./..." }
				}
			]
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pipelines/validate", bytes.NewReader(payload))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["valid"] != true {
		t.Fatalf("expected valid true, got %#v", body)
	}
}
