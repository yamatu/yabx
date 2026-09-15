package node

import (
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
)

func TestNormalizedPushIntervalKeepsOnlineStatusWindow(t *testing.T) {
	cases := []struct {
		name      string
		panelType string
		interval  time.Duration
		want      time.Duration
	}{
		// XBoard marks a user online when users.t is younger than 120s, so the
		// push cycle must stay below that window.
		{"xboard clamps long interval", "Xboard", 4 * time.Minute, maxPanelPushInterval},
		{"xboard keeps default", "Xboard", 60 * time.Second, 60 * time.Second},
		{"xboard raises tiny interval", "Xboard", 5 * time.Second, 20 * time.Second},
		{"xboard defaults zero interval", "Xboard", 0, 60 * time.Second},
		// ppanel manages its own online state, keep the configured interval.
		{"ppanel keeps long interval", "ppanel", 10 * time.Minute, 10 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Controller{apiClient: &panel.Client{PanelType: tc.panelType}}
			if got := c.normalizedPushInterval(tc.interval); got != tc.want {
				t.Fatalf("normalizedPushInterval(%s) = %s, want %s", tc.interval, got, tc.want)
			}
		})
	}
}
