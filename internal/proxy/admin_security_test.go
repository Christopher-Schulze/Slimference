package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/security"
)

// testProxyWithDetector returns a Proxy with only the fields the
// security-suspend handler consults populated. Other fields are nil so we
// do not spin up a full proxy for a targeted handler test.
func testProxyWithDetector(t *testing.T) *Proxy {
	t.Helper()
	return &Proxy{
		secretsDetector: security.NewDetector("redact", nil, nil),
	}
}

func TestAdminSecuritySuspend_Post_SetsDeadline(t *testing.T) {
	p := testProxyWithDetector(t)
	body, _ := json.Marshal(AdminSecuritySuspendRequest{SuspendSeconds: 300})
	req := httptest.NewRequest(http.MethodPost, AdminSecuritySuspendPath,
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp AdminSecuritySuspendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Active {
		t.Fatal("expected active=true")
	}
	if resp.UntilUnixSec == 0 {
		t.Fatal("UntilUnixSec not set")
	}
	if resp.Mode != "redact" {
		t.Fatalf("mode = %q, want redact", resp.Mode)
	}
}

func TestAdminSecuritySuspend_Post_ZeroClears(t *testing.T) {
	p := testProxyWithDetector(t)
	p.secretsDetector.SuspendUntil(time.Now().Add(5 * time.Minute))

	body, _ := json.Marshal(AdminSecuritySuspendRequest{SuspendSeconds: 0})
	req := httptest.NewRequest(http.MethodPost, AdminSecuritySuspendPath,
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)

	var resp AdminSecuritySuspendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Active {
		t.Fatal("suspend_seconds=0 did not clear")
	}
}

func TestAdminSecuritySuspend_Get_ReportsState(t *testing.T) {
	p := testProxyWithDetector(t)
	p.secretsDetector.SuspendUntil(time.Now().Add(10 * time.Minute))

	req := httptest.NewRequest(http.MethodGet, AdminSecuritySuspendPath, nil)
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)

	var resp AdminSecuritySuspendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Active {
		t.Fatal("GET did not report active suspension")
	}
}

func TestAdminSecuritySuspend_NoDetectorReturns503(t *testing.T) {
	p := &Proxy{secretsDetector: nil}
	req := httptest.NewRequest(http.MethodPost, AdminSecuritySuspendPath,
		bytes.NewReader([]byte(`{"suspend_seconds":60}`)))
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
}

func TestAdminSecuritySuspend_InvalidJSONReturns400(t *testing.T) {
	p := testProxyWithDetector(t)
	req := httptest.NewRequest(http.MethodPost, AdminSecuritySuspendPath,
		bytes.NewReader([]byte("not-json")))
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestAdminSecuritySuspend_ClampsTo1Hour(t *testing.T) {
	p := testProxyWithDetector(t)
	body, _ := json.Marshal(AdminSecuritySuspendRequest{
		SuspendSeconds: 3600 * 24, // 24h
	})
	req := httptest.NewRequest(http.MethodPost, AdminSecuritySuspendPath,
		bytes.NewReader(body))
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)

	var resp AdminSecuritySuspendResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.UntilUnixSec == 0 {
		t.Fatal("no deadline returned")
	}
	// Must be within 1h + 2s of now.
	max := time.Now().Add(time.Hour).Unix() + 2
	if resp.UntilUnixSec > max {
		t.Fatalf("deadline %d exceeds 1h clamp (max %d)", resp.UntilUnixSec, max)
	}
}

func TestAdminSecuritySuspend_DeleteReturns405(t *testing.T) {
	p := testProxyWithDetector(t)
	req := httptest.NewRequest(http.MethodDelete, AdminSecuritySuspendPath, nil)
	rec := httptest.NewRecorder()
	p.adminSecuritySuspendHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
}
