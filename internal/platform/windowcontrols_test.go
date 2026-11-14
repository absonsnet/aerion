package platform

import (
	"reflect"
	"testing"
)

func TestParseGsettingsButtonLayout(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantSide  string
		wantOrder []string
		wantOK    bool
	}{
		{
			name:      "Ubuntu default is left",
			raw:       "'close,minimize,maximize:appmenu'",
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name:      "GNOME default is right",
			raw:       "'appmenu:close,minimize,maximize'",
			wantSide:  WindowControlsRight,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name:      "GNOME right order preserved",
			raw:       "'appmenu:minimize,maximize,close'",
			wantSide:  WindowControlsRight,
			wantOrder: []string{"minimize", "maximize", "close"},
			wantOK:    true,
		},
		{
			name:      "unquoted value is accepted",
			raw:       "appmenu:minimize,maximize,close",
			wantSide:  WindowControlsRight,
			wantOrder: []string{"minimize", "maximize", "close"},
			wantOK:    true,
		},
		{
			name:      "left order preserved when unusual",
			raw:       "'maximize,minimize,close:appmenu'",
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"maximize", "minimize", "close"},
			wantOK:    true,
		},
		{
			name:      "subset is completed to all three buttons",
			raw:       "'close:appmenu'",
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name:   "empty value is not ok",
			raw:    "''",
			wantOK: false,
		},
		{
			name:   "blank value is not ok",
			raw:    "   ",
			wantOK: false,
		},
		{
			name:   "no close button anywhere is not ok",
			raw:    "'appmenu:minimize,maximize'",
			wantOK: false,
		},
		{
			name:   "menu only is not ok",
			raw:    "'appmenu:'",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			side, order, ok := parseGsettingsButtonLayout(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (side=%q order=%v)", ok, tc.wantOK, side, order)
			}
			if !tc.wantOK {
				return
			}
			if side != tc.wantSide {
				t.Errorf("side = %q, want %q", side, tc.wantSide)
			}
			if !reflect.DeepEqual(order, tc.wantOrder) {
				t.Errorf("order = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}

func TestParseKwinrcButtons(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantSide  string
		wantOrder []string
		wantOK    bool
	}{
		{
			name: "KDE default is right",
			content: `[org.kde.kdecoration2]
ButtonsOnLeft=MS
ButtonsOnRight=HIAX
`,
			wantSide:  WindowControlsRight,
			wantOrder: []string{"minimize", "maximize", "close"},
			wantOK:    true,
		},
		{
			name: "close on the left",
			content: `[org.kde.kdecoration2]
ButtonsOnLeft=XIA
ButtonsOnRight=H
`,
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name: "comma-separated codes are tolerated",
			content: `[org.kde.kdecoration2]
ButtonsOnLeft=M,S
ButtonsOnRight=H,I,A,X
`,
			wantSide:  WindowControlsRight,
			wantOrder: []string{"minimize", "maximize", "close"},
			wantOK:    true,
		},
		{
			name: "other sections are ignored",
			content: `[General]
ButtonsOnLeft=X
[org.kde.kdecoration2]
ButtonsOnLeft=MS
ButtonsOnRight=IAX
`,
			wantSide:  WindowControlsRight,
			wantOrder: []string{"minimize", "maximize", "close"},
			wantOK:    true,
		},
		{
			name: "subset is completed",
			content: `[org.kde.kdecoration2]
ButtonsOnLeft=X
`,
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name: "missing section is not ok",
			content: `[General]
ButtonsOnRight=HIAX
`,
			wantOK: false,
		},
		{
			name: "no close button is not ok",
			content: `[org.kde.kdecoration2]
ButtonsOnLeft=MS
ButtonsOnRight=HIA
`,
			wantOK: false,
		},
		{
			name:    "empty file is not ok",
			content: "",
			wantOK:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			side, order, ok := parseKwinrcButtons(tc.content)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (side=%q order=%v)", ok, tc.wantOK, side, order)
			}
			if !tc.wantOK {
				return
			}
			if side != tc.wantSide {
				t.Errorf("side = %q, want %q", side, tc.wantSide)
			}
			if !reflect.DeepEqual(order, tc.wantOrder) {
				t.Errorf("order = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}

func TestParseXfconfButtonLayout(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantSide  string
		wantOrder []string
		wantOK    bool
	}{
		{
			name:      "XFCE default is right",
			raw:       "O|HMC",
			wantSide:  WindowControlsRight,
			wantOrder: []string{"minimize", "maximize", "close"},
			wantOK:    true,
		},
		{
			name:      "close on the left",
			raw:       "CHM|O",
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name:      "left order preserved when unusual",
			raw:       "MHC|O",
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"maximize", "minimize", "close"},
			wantOK:    true,
		},
		{
			name:      "subset is completed",
			raw:       "C|O",
			wantSide:  WindowControlsLeft,
			wantOrder: []string{"close", "minimize", "maximize"},
			wantOK:    true,
		},
		{
			name:   "empty value is not ok",
			raw:    "",
			wantOK: false,
		},
		{
			name:   "no close button is not ok",
			raw:    "O|HM",
			wantOK: false,
		},
		{
			name:   "no separator and no close is not ok",
			raw:    "OHM",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			side, order, ok := parseXfconfButtonLayout(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (side=%q order=%v)", ok, tc.wantOK, side, order)
			}
			if !tc.wantOK {
				return
			}
			if side != tc.wantSide {
				t.Errorf("side = %q, want %q", side, tc.wantSide)
			}
			if !reflect.DeepEqual(order, tc.wantOrder) {
				t.Errorf("order = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}

func TestCompleteOrder(t *testing.T) {
	tests := []struct {
		name  string
		side  string
		found []string
		want  []string
	}{
		{"left full", WindowControlsLeft, []string{"close", "minimize", "maximize"}, []string{"close", "minimize", "maximize"}},
		{"left subset keeps listed order first", WindowControlsLeft, []string{"maximize", "close"}, []string{"maximize", "close", "minimize"}},
		{"right subset", WindowControlsRight, []string{"close"}, []string{"close", "minimize", "maximize"}},
		{"right empty", WindowControlsRight, nil, []string{"minimize", "maximize", "close"}},
		{"left empty", WindowControlsLeft, nil, []string{"close", "minimize", "maximize"}},
		{"duplicates are removed", WindowControlsLeft, []string{"close", "close", "minimize"}, []string{"close", "minimize", "maximize"}},
		{"unknown names are ignored", WindowControlsRight, []string{"appmenu", "minimize", "close"}, []string{"minimize", "close", "maximize"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := completeOrder(tc.side, tc.found)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("completeOrder(%q, %v) = %v, want %v", tc.side, tc.found, got, tc.want)
			}
		})
	}
}

func TestLayoutFromDesktopHint(t *testing.T) {
	tests := []struct {
		name      string
		de        string
		wantSide  string
		wantOrder []string
		wantOK    bool
	}{
		{"ubuntu", "ubuntu", WindowControlsLeft, []string{"close", "minimize", "maximize"}, true},
		{"ubuntu with GNOME suffix", "ubuntu:gnome", WindowControlsLeft, []string{"close", "minimize", "maximize"}, true},
		{"gnome alone is not a left hint", "gnome", "", nil, false},
		{"kde is not a left hint", "kde", "", nil, false},
		{"empty is not a hint", "", "", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			side, order, ok := layoutFromDesktopHint(tc.de)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if side != tc.wantSide {
				t.Errorf("side = %q, want %q", side, tc.wantSide)
			}
			if !reflect.DeepEqual(order, tc.wantOrder) {
				t.Errorf("order = %v, want %v", order, tc.wantOrder)
			}
		})
	}
}

func TestFallbackLayout(t *testing.T) {
	l := fallbackLayout()
	if l.Side != WindowControlsRight {
		t.Errorf("Side = %q, want %q", l.Side, WindowControlsRight)
	}
	if !reflect.DeepEqual(l.Order, []string{"minimize", "maximize", "close"}) {
		t.Errorf("Order = %v, want [minimize maximize close]", l.Order)
	}
	if l.Style != WindowControlsTraffic {
		t.Errorf("Style = %q, want %q", l.Style, WindowControlsTraffic)
	}
}

// DetectWindowControlsLayout must always return a usable layout, even on
// a machine with no desktop environment at all (CI, headless test runs).
// The exact side depends on the machine, so only validity is asserted.
func TestDetectWindowControlsLayoutIsValid(t *testing.T) {
	l := DetectWindowControlsLayout()
	if l.Side != WindowControlsLeft && l.Side != WindowControlsRight {
		t.Errorf("Side = %q, want left or right", l.Side)
	}
	if len(l.Order) != 3 {
		t.Fatalf("Order = %v, want exactly three buttons", l.Order)
	}
	for _, b := range l.Order {
		if b != "close" && b != "minimize" && b != "maximize" {
			t.Errorf("Order contains unknown button %q", b)
		}
	}
	if l.Style != WindowControlsTraffic && l.Style != WindowControlsGlyphs {
		t.Errorf("Style = %q, want traffic or glyphs", l.Style)
	}
}
