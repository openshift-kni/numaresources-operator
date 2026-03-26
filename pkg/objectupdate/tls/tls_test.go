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
