package rterm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dev6699/rterm/provider"
)

func TestRegisterForwardsRootProviderAPIPath(t *testing.T) {
	previousPrefix := defaultPrefix
	t.Cleanup(func() { defaultPrefix = previousPrefix })
	SetPrefix("/")

	mux := http.NewServeMux()
	if err := register(mux, nil, provider.NewService(provider.Config{})); err != nil {
		t.Fatal(err)
	}
	recording := httptest.NewRecorder()
	mux.ServeHTTP(recording, httptest.NewRequest(http.MethodGet, "/api/providers", nil))

	if recording.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recording.Code, http.StatusOK)
	}
}

func TestCommandPageUsesConfiguredAssetPrefix(t *testing.T) {
	previousPrefix := defaultPrefix
	t.Cleanup(func() { defaultPrefix = previousPrefix })
	SetPrefix("/rterm")

	mux := http.NewServeMux()
	if err := register(mux, []Command{{Name: "bash"}}, nil); err != nil {
		t.Fatal(err)
	}
	recording := httptest.NewRecorder()
	mux.ServeHTTP(recording, httptest.NewRequest(http.MethodGet, "/rterm/bash", nil))
	body := recording.Body.String()

	if recording.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recording.Code, http.StatusOK)
	}
	for _, asset := range []string{"/rterm/xterm.css", "/rterm/style.css", "/rterm/xterm.js", "/rterm/addon-fit.js", "/rterm/script.js"} {
		if !strings.Contains(body, asset) {
			t.Errorf("page does not contain prefixed asset %q", asset)
		}
	}
	for _, asset := range []string{"state.js", "protocol.js", "bridge.js", "terminal.js", "auth.js", "transfers.js", "sessions.js", "sockets.js", "provider.js"} {
		assetRecording := httptest.NewRecorder()
		mux.ServeHTTP(assetRecording, httptest.NewRequest(http.MethodGet, "/rterm/"+asset, nil))
		if assetRecording.Code != http.StatusOK {
			t.Errorf("asset %q returned status %d", asset, assetRecording.Code)
		}
	}
	if strings.Contains(body, "__RTERM_PREFIX__") {
		t.Error("page contains unreplaced asset prefix placeholder")
	}
}
