package cmd

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestRun(t *testing.T) {
	Run()
}

func TestSetLogLevel(t *testing.T) {
	originalLevel := log.GetLevel()
	defer log.SetLevel(originalLevel)

	cases := []struct {
		level string
		want  log.Level
	}{
		// Xray spells these differently than logrus, and a "warning" config used
		// to be ignored silently because the switch had no default branch.
		{"warning", log.WarnLevel},
		{"warn", log.WarnLevel},
		{"trace", log.TraceLevel},
		{"debug", log.DebugLevel},
		{"info", log.InfoLevel},
		{"", log.InfoLevel},
		{"error", log.ErrorLevel},
		{"fatal", log.FatalLevel},
		{"panic", log.PanicLevel},
		{"WARN", log.WarnLevel},
		{" warning ", log.WarnLevel},
	}

	for _, tc := range cases {
		t.Run("level="+tc.level, func(t *testing.T) {
			log.SetLevel(log.PanicLevel)
			setLogLevel(tc.level)
			if got := log.GetLevel(); got != tc.want {
				t.Fatalf("setLogLevel(%q) = %s, want %s", tc.level, got, tc.want)
			}
		})
	}
}

func TestSetLogLevelReportsUnknownValue(t *testing.T) {
	originalLevel := log.GetLevel()
	originalOutput := log.StandardLogger().Out
	originalFormatter := log.StandardLogger().Formatter
	defer func() {
		log.SetLevel(originalLevel)
		log.SetOutput(originalOutput)
		log.SetFormatter(originalFormatter)
	}()

	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	log.SetFormatter(&log.TextFormatter{DisableTimestamp: true})

	setLogLevel("verbose")

	if got := log.GetLevel(); got != log.InfoLevel {
		t.Fatalf("setLogLevel(\"verbose\") = %s, want info", got)
	}
	if logged := buf.String(); !strings.Contains(logged, "Unknown log level") {
		t.Fatalf("logged = %q, want a warning about the unknown level", logged)
	}
}
