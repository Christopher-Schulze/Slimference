package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/types"
)

func TestBypass_ShortCircuitsProviderEnabled(t *testing.T) {
	p := &Proxy{}
	p.providerEnabled[types.Anthropic].Store(true)
	p.providerEnabled[types.CodexChatGPT].Store(true)
	if !p.isProviderEnabled(types.Anthropic) {
		t.Fatal("sanity: provider should be enabled pre-bypass")
	}
	p.SetBypass(true)
	if p.isProviderEnabled(types.Anthropic) {
		t.Fatal("bypass should disable Anthropic")
	}
	if p.isProviderEnabled(types.CodexChatGPT) {
		t.Fatal("bypass should disable CodexChatGPT")
	}
	p.SetBypass(false)
	if !p.isProviderEnabled(types.Anthropic) {
		t.Fatal("clearing bypass should restore providerEnabled state")
	}
}

func TestBypass_ShortCircuitsLayerEnabled(t *testing.T) {
	p := &Proxy{}
	for i := range p.layerEnabled {
		p.layerEnabled[i].Store(true)
	}
	if !p.isLayerEnabled(1) || !p.isLayerEnabled(3) {
		t.Fatal("sanity: layers enabled pre-bypass")
	}
	p.SetBypass(true)
	for _, i := range []int{1, 3} {
		if p.isLayerEnabled(i) {
			t.Fatalf("layer %d should be disabled under bypass", i)
		}
	}
}

func TestAdminBypass_PostToggles(t *testing.T) {
	p := &Proxy{}
	body, _ := json.Marshal(AdminBypassRequest{Enabled: true})
	req := httptest.NewRequest(http.MethodPost, AdminBypassPath,
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.adminBypassHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp AdminBypassResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Enabled {
		t.Fatal("bypass not enabled after POST")
	}
	if !p.Bypass() {
		t.Fatal("Bypass() disagrees with response")
	}

	// Now disable.
	body, _ = json.Marshal(AdminBypassRequest{Enabled: false})
	req = httptest.NewRequest(http.MethodPost, AdminBypassPath,
		bytes.NewReader(body))
	rec = httptest.NewRecorder()
	p.adminBypassHandler(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Enabled {
		t.Fatal("second POST did not clear bypass")
	}
}

func TestAdminBypass_GetReportsState(t *testing.T) {
	p := &Proxy{}
	p.SetBypass(true)
	req := httptest.NewRequest(http.MethodGet, AdminBypassPath, nil)
	rec := httptest.NewRecorder()
	p.adminBypassHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp AdminBypassResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Enabled {
		t.Fatal("GET did not reflect bypass=true")
	}
}

func TestAdminBypass_InvalidJSON400(t *testing.T) {
	p := &Proxy{}
	req := httptest.NewRequest(http.MethodPost, AdminBypassPath,
		bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	p.adminBypassHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestAdminBypass_PostWithDuration(t *testing.T) {
	p := New(config.Defaults())
	body := `{"enabled":true,"duration_seconds":30}`
	req := httptest.NewRequest(http.MethodPost, AdminBypassPath,
		bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	p.adminBypassHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if !p.Bypass() {
		t.Fatal("bypass must be on")
	}
	if p.BypassExpiresAt().IsZero() {
		t.Fatal("expected non-zero deadline")
	}
}

func TestAdminBypass_PostWithNextRequests(t *testing.T) {
	p := New(config.Defaults())
	body := `{"enabled":true,"next_requests":4}`
	req := httptest.NewRequest(http.MethodPost, AdminBypassPath,
		bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	p.adminBypassHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if p.BypassNextRequestCount() != 4 {
		t.Fatalf("expected budget 4, got %d", p.BypassNextRequestCount())
	}
}

func TestAdminBypass_Delete405(t *testing.T) {
	p := &Proxy{}
	req := httptest.NewRequest(http.MethodDelete, AdminBypassPath, nil)
	rec := httptest.NewRecorder()
	p.adminBypassHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d", rec.Code)
	}
}
