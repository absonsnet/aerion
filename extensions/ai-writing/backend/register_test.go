package backend

import (
	"errors"
	"testing"

	aiwriting "github.com/absonsnet/daak/extensions/ai-writing"
	coreapi "github.com/absonsnet/daak/internal/core/api/v1"
)

type stubUI struct {
	coreapi.UI // embedded: all other UI methods nil-panic if called

	toolbarReq  *coreapi.ComposerToolbarRequest
	settingsReq *coreapi.SettingsTabRequest
	toolbarErr  error
	settingsErr error

	toolbarUnregistered  bool
	settingsUnregistered bool
}

func (u *stubUI) RegisterComposerToolbar(req coreapi.ComposerToolbarRequest) (coreapi.Unregister, error) {
	if u.toolbarErr != nil {
		return nil, u.toolbarErr
	}
	u.toolbarReq = &req
	return func() { u.toolbarUnregistered = true }, nil
}

func (u *stubUI) RegisterSettingsTab(req coreapi.SettingsTabRequest) (coreapi.Unregister, error) {
	if u.settingsErr != nil {
		return nil, u.settingsErr
	}
	u.settingsReq = &req
	return func() { u.settingsUnregistered = true }, nil
}

type stubRegCore struct {
	coreapi.Core
	ui coreapi.UI
}

func (c *stubRegCore) UI() coreapi.UI { return c.ui }

func TestManifest(t *testing.T) {
	m := aiwriting.Manifest()
	if m.ID != "ai-writing" || m.Name != "AI Writing Assistant" || m.Version != "0.1.0" {
		t.Errorf("manifest = %+v", m)
	}
	if got := string(aiwriting.ManifestJSON()); got == "" {
		t.Errorf("ManifestJSON empty")
	}
}

func TestRegister_Success(t *testing.T) {
	ui := &stubUI{}
	ext := NewExtension()
	unreg, err := ext.Register(&stubRegCore{ui: ui})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if ui.toolbarReq == nil || ui.toolbarReq.ExtensionID != "ai-writing" ||
		ui.toolbarReq.Label != "Improve Writing" || ui.toolbarReq.Icon != "mdi:pencil" ||
		ui.toolbarReq.Component != "AIWritingButton" {
		t.Errorf("toolbar req = %+v", ui.toolbarReq)
	}
	if ui.settingsReq == nil || ui.settingsReq.ExtensionID != "ai-writing" ||
		ui.settingsReq.Label != "AI Writing Assistant" || ui.settingsReq.Component != "AIWritingSettingsDialog" {
		t.Errorf("settings req = %+v", ui.settingsReq)
	}
	// The returned unregister rolls both back.
	unreg()
	if !ui.toolbarUnregistered || !ui.settingsUnregistered {
		t.Errorf("unregister did not roll back both: toolbar=%v settings=%v",
			ui.toolbarUnregistered, ui.settingsUnregistered)
	}
}

func TestRegister_SettingsFailureRollsBackToolbar(t *testing.T) {
	ui := &stubUI{settingsErr: errors.New("settings tab boom")}
	ext := NewExtension()
	if _, err := ext.Register(&stubRegCore{ui: ui}); err == nil {
		t.Fatal("expected error")
	}
	if !ui.toolbarUnregistered {
		t.Errorf("toolbar registration was not rolled back after settings failure")
	}
}

func TestRegister_ToolbarFailure(t *testing.T) {
	ui := &stubUI{toolbarErr: errors.New("toolbar boom")}
	ext := NewExtension()
	if _, err := ext.Register(&stubRegCore{ui: ui}); err == nil {
		t.Fatal("expected error")
	}
	if ui.settingsReq != nil {
		t.Errorf("settings tab must not be registered after toolbar failure")
	}
}
