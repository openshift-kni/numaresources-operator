/*
 * Copyright 2026 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package tls

import (
	"crypto/tls"
	"testing"
)

func TestNewSettingsMinVersion(t *testing.T) {
	type testCase struct {
		version  uint16
		expected string
	}

	testCases := []testCase{
		{tls.VersionTLS10, "VersionTLS10"},
		{tls.VersionTLS11, "VersionTLS11"},
		{tls.VersionTLS12, "VersionTLS12"},
		{tls.VersionTLS13, "VersionTLS13"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			s := NewSettings(&tls.Config{
				MinVersion:   tc.version,
				CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			})
			if s.MinVersion != tc.expected {
				t.Errorf("MinVersion: got %q, expected %q", s.MinVersion, tc.expected)
			}
		})
	}
}

func TestNewSettingsCipherSuites(t *testing.T) {
	ciphers := []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	}

	s := NewSettings(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: ciphers,
	})

	expected := "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
	if s.CipherSuites != expected {
		t.Errorf("CipherSuites: got %q, expected %q", s.CipherSuites, expected)
	}
}

func TestNewSettingsCipherSuitesEmpty(t *testing.T) {
	s := NewSettings(&tls.Config{
		MinVersion: tls.VersionTLS12,
	})

	if s.CipherSuites != "" {
		t.Errorf("CipherSuites: got %q, expected empty string", s.CipherSuites)
	}
}

func TestNewSettingsCipherSuitesSingle(t *testing.T) {
	s := NewSettings(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_AES_128_GCM_SHA256},
	})

	expected := "TLS_AES_128_GCM_SHA256"
	if s.CipherSuites != expected {
		t.Errorf("CipherSuites: got %q, expected %q", s.CipherSuites, expected)
	}
}

func TestNewSettings_WithCurvePreferences(t *testing.T) {
	tests := []struct {
		name           string
		curveIDs       []tls.CurveID
		wantCurvePrefs string
	}{
		{
			name:           "Intermediate profile",
			curveIDs:       []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
			wantCurvePrefs: "29,23,24",
		},
		{
			name:           "Modern profile with PQ",
			curveIDs:       []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256},
			wantCurvePrefs: "4588,29,23",
		},
		{
			name:           "Empty curves (Legacy)",
			curveIDs:       []tls.CurveID{},
			wantCurvePrefs: "",
		},
		{
			name:           "Only PQ curve",
			curveIDs:       []tls.CurveID{tls.X25519MLKEM768},
			wantCurvePrefs: "4588",
		},
		{
			name:           "Only FIPS-approved curves",
			curveIDs:       []tls.CurveID{tls.CurveP256, tls.CurveP384, tls.CurveP521},
			wantCurvePrefs: "23,24,25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &tls.Config{
				MinVersion:       tls.VersionTLS12,
				CipherSuites:     []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
				CurvePreferences: tt.curveIDs,
			}
			settings := NewSettings(cfg)

			if settings.CurvePreferences != tt.wantCurvePrefs {
				t.Errorf("CurvePreferences = %q, want %q",
					settings.CurvePreferences, tt.wantCurvePrefs)
			}

			if settings.MinVersion == "" {
				t.Error("MinVersion should not be empty")
			}

			if settings.CipherSuites == "" {
				t.Error("CipherSuites should not be empty")
			}
		})
	}
}

func TestSettings_String(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     string
	}{
		{
			name: "with curves",
			settings: Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23,24",
			},
			want: "MinVersion: VersionTLS12, CipherSuites: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, CurvePreferences: 29,23,24",
		},
		{
			name: "without curves",
			settings: Settings{
				MinVersion:       "VersionTLS13",
				CipherSuites:     "",
				CurvePreferences: "",
			},
			want: "MinVersion: VersionTLS13, CipherSuites: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.settings.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
