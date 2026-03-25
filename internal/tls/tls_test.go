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

func TestValidate(t *testing.T) {
	type testCase struct {
		name               string
		tlsConfig          func(*tls.Config)
		unsupportedCiphers []string
		expectErr          bool
	}

	testCases := []testCase{
		{
			name: "valid TLS 1.2 with ciphers",
			tlsConfig: func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS12
				c.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
			},
		},
		{
			name: "valid TLS 1.3 with ciphers",
			tlsConfig: func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS13
				c.CipherSuites = []uint16{tls.TLS_AES_128_GCM_SHA256}
			},
		},
		{
			name:      "nil callback returns error",
			tlsConfig: nil,
			expectErr: true,
		},
		{
			name: "callback that sets nothing returns error",
			tlsConfig: func(c *tls.Config) {
			},
			expectErr: true,
		},
		{
			name: "zero MinVersion returns error",
			tlsConfig: func(c *tls.Config) {
				c.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
			},
			expectErr: true,
		},
		{
			name: "empty CipherSuites returns error",
			tlsConfig: func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS12
			},
			expectErr: true,
		},
		{
			name: "unsupported MinVersion returns error",
			tlsConfig: func(c *tls.Config) {
				c.MinVersion = 0x9999
				c.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
			},
			expectErr: true,
		},
		{
			name: "unsupported ciphers logged but not an error",
			tlsConfig: func(c *tls.Config) {
				c.MinVersion = tls.VersionTLS12
				c.CipherSuites = []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}
			},
			unsupportedCiphers: []string{"FAKE_CIPHER_1", "FAKE_CIPHER_2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfig(tc.tlsConfig, tc.unsupportedCiphers)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTLSVersionToString(t *testing.T) {
	type testCase struct {
		version  uint16
		expected string
	}

	testCases := []testCase{
		{tls.VersionTLS10, "VersionTLS10"},
		{tls.VersionTLS11, "VersionTLS11"},
		{tls.VersionTLS12, "VersionTLS12"},
		{tls.VersionTLS13, "VersionTLS13"},
		{0, ""},
		{0x9999, ""},
	}

	for _, tc := range testCases {
		got := VersionToString(tc.version)
		if got != tc.expected {
			t.Errorf("TLSVersionToString(%d): got %q, expected %q", tc.version, got, tc.expected)
		}
	}
}

func TestCipherSuitesToString(t *testing.T) {
	ciphers := []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	}

	got := CipherSuitesToString(ciphers)
	expected := []string{
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
	}

	if len(got) != len(expected) {
		t.Fatalf("length mismatch: got %d, expected %d", len(got), len(expected))
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Errorf("index %d: got %q, expected %q", i, got[i], expected[i])
		}
	}
}

func TestCipherSuitesToStringEmpty(t *testing.T) {
	got := CipherSuitesToString(nil)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}
