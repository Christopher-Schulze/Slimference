package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/control"
	"github.com/slimference/slimference/internal/control/apps"
)

// adminStateProvider holds the lazily-constructed control.Probes used
// by the /admin/state handler. It is wired by the cmd/slimference
// entrypoint after Proxy.New so the proxy struct does not need to
// import the control sub-system directly.
//
// Reads and writes are atomic so the handler can run concurrently with
// startup wiring or hot-reload.
type adminStateProvider struct {
	probes atomic.Pointer[control.Probes]
}

// SetStateProvider installs the probe set used by /admin/state.
// Safe to call concurrently. Passing a nil probes pointer clears the
// provider (the handler then responds 503).
func (p *Proxy) SetStateProvider(probes *control.Probes) {
	p.adminState.probes.Store(probes)
}

// stateProbes returns the currently-installed probe set or nil when
// none has been wired. nil is fine - the handler reports 503.
func (p *Proxy) stateProbes() *control.Probes {
	return p.adminState.probes.Load()
}

func (p *Proxy) SetupStateSnapshot(ctx context.Context) (control.SetupState, bool) {
	probes := p.stateProbes()
	if probes == nil {
		return control.SetupState{}, false
	}
	state := control.Build(ctx, *probes)
	p.hostBudgetExceeded.Store(state.HostBudget.Exceeded)
	return state, true
}

// adminStateHandler returns the current SetupState as JSON. GET only.
func (p *Proxy) adminStateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAdminJSON(w, http.StatusMethodNotAllowed, adminActionResponse{OK: false, Error: "GET required"})
		return
	}
	probes := p.stateProbes()
	if probes == nil {
		writeAdminJSON(w, http.StatusServiceUnavailable, adminActionResponse{OK: false, Error: "state provider not wired"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
	defer cancel()
	state, _ := p.SetupStateSnapshot(ctx)
	writeAdminJSON(w, http.StatusOK, state)
}

// AdminAppsRequest is the POST body for toggling a single app's
// routing-enabled flag.
type AdminAppsRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// AdminAppsResponse echoes the policy after the change so the caller
// can render the resulting state in one round-trip.
type AdminAppsResponse struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Enabled map[string]bool `json:"enabled,omitempty"`
}

// adminAppsHandler exposes the per-app policy. GET returns the
// current Enabled map; POST sets one app's flag and returns the new
// Enabled map.
func (p *Proxy) adminAppsHandler(w http.ResponseWriter, r *http.Request) {
	m := p.AppsManager()
	if m == nil {
		writeAdminJSON(w, http.StatusServiceUnavailable, AdminAppsResponse{OK: false, Error: "apps manager not wired"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeAdminJSON(w, http.StatusOK, AdminAppsResponse{
			OK:      true,
			Enabled: policyEnabledMap(m.Policy()),
		})
	case http.MethodPost:
		var req AdminAppsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAdminJSON(w, http.StatusBadRequest, AdminAppsResponse{OK: false, Error: "invalid JSON"})
			return
		}
		id := apps.AppID(req.ID)
		if !id.IsKnown() {
			writeAdminJSON(w, http.StatusBadRequest, AdminAppsResponse{OK: false, Error: "unknown app id"})
			return
		}
		if id == apps.AppClaudeCode && req.Enabled {
			writeAdminJSON(w, http.StatusBadRequest, AdminAppsResponse{OK: false, Error: "claude_code is parked; Slimference is Codex-only"})
			return
		}
		if err := m.SetEnabled(id, req.Enabled); err != nil {
			writeAdminJSON(w, http.StatusInternalServerError, AdminAppsResponse{OK: false, Error: err.Error()})
			return
		}
		writeAdminJSON(w, http.StatusOK, AdminAppsResponse{
			OK:      true,
			Enabled: policyEnabledMap(m.Policy()),
		})
	default:
		writeAdminJSON(w, http.StatusMethodNotAllowed, AdminAppsResponse{OK: false, Error: "GET or POST"})
	}
}

// policyEnabledMap renders the Policy.Enabled map with string keys so
// the JSON encoding stays stable across AppID identifier changes.
func policyEnabledMap(pol apps.Policy) map[string]bool {
	out := make(map[string]bool, len(apps.KnownApps))
	for _, id := range apps.KnownApps {
		out[string(id)] = pol.IsEnabled(id)
	}
	return out
}
