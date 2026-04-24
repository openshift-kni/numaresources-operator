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
	"errors"
	"fmt"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

func TestWrapTLSHandshakeError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		if got := wrapTLSHandshakeError(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("tls.AlertError wraps as server rejected", func(t *testing.T) {
		alertErr := tls.AlertError(40) // handshake_failure alert
		got := wrapTLSHandshakeError(alertErr)
		if !errors.Is(got, ErrTLSServerRejected) {
			t.Errorf("expected ErrTLSServerRejected, got %v", got)
		}
		if !errors.Is(got, ErrTLSHandshakeRejected) {
			t.Errorf("expected ErrTLSHandshakeRejected in chain, got %v", got)
		}
	})

	t.Run("handshake failure string wraps as server rejected", func(t *testing.T) {
		err := fmt.Errorf("remote error: tls: handshake failure")
		got := wrapTLSHandshakeError(err)
		if !errors.Is(got, ErrTLSServerRejected) {
			t.Errorf("expected ErrTLSServerRejected, got %v", got)
		}
	})

	t.Run("protocol version string wraps as server rejected", func(t *testing.T) {
		err := fmt.Errorf("tls: no supported protocol version")
		got := wrapTLSHandshakeError(err)
		if !errors.Is(got, ErrTLSServerRejected) {
			t.Errorf("expected ErrTLSServerRejected, got %v", got)
		}
	})

	t.Run("no supported versions wraps as client config incompatible", func(t *testing.T) {
		err := fmt.Errorf("tls: no supported versions satisfy MinVersion and MaxVersion")
		got := wrapTLSHandshakeError(err)
		if !errors.Is(got, ErrTLSClientConfigIncompatible) {
			t.Errorf("expected ErrTLSClientConfigIncompatible, got %v", got)
		}
		if !errors.Is(got, ErrTLSHandshakeRejected) {
			t.Errorf("expected ErrTLSHandshakeRejected in chain, got %v", got)
		}
	})

	t.Run("no ciphers available wraps as client config incompatible", func(t *testing.T) {
		err := fmt.Errorf("tls: no ciphers available")
		got := wrapTLSHandshakeError(err)
		if !errors.Is(got, ErrTLSClientConfigIncompatible) {
			t.Errorf("expected ErrTLSClientConfigIncompatible, got %v", got)
		}
	})

	t.Run("unrelated error is returned as-is", func(t *testing.T) {
		err := fmt.Errorf("connection refused")
		got := wrapTLSHandshakeError(err)
		if errors.Is(got, ErrTLSHandshakeRejected) {
			t.Errorf("should not wrap unrelated error, got %v", got)
		}
		if got.Error() != err.Error() {
			t.Errorf("expected original error %q, got %q", err, got)
		}
	})
}

func TestFindDisallowedCipher(t *testing.T) {
	oldProfile := configv1.TLSProfiles[configv1.TLSProfileOldType]
	intermediateProfile := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

	t.Run("returns empty when all ciphers are allowed", func(t *testing.T) {
		got := FindDisallowedCipher(oldProfile.Ciphers)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("finds a disallowed cipher for intermediate profile", func(t *testing.T) {
		got := FindDisallowedCipher(intermediateProfile.Ciphers)
		if got == "" {
			t.Fatal("expected a disallowed cipher, got empty")
		}
		for _, c := range intermediateProfile.Ciphers {
			if c == got {
				t.Errorf("returned cipher %q should not be in the allowed set", got)
			}
		}
	})

	t.Run("skips TLS 1.3 ciphers", func(t *testing.T) {
		// allow nothing — first result must still be a TLS 1.2 cipher
		got := FindDisallowedCipher(nil)
		if got == "" {
			t.Fatal("expected a disallowed cipher, got empty")
		}
		if len(got) >= 4 && got[:4] == "TLS_" {
			t.Errorf("returned cipher %q is a TLS 1.3 cipher, should have been skipped", got)
		}
	})

	t.Run("returns empty when allowed is nil but old profile has no TLS 1.2 ciphers", func(t *testing.T) {
		// This case can't actually happen with real profiles, but tests
		// the logic: if we pass all old ciphers as allowed, nothing is disallowed.
		got := FindDisallowedCipher(oldProfile.Ciphers)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestOpenSSLCipherToGoID(t *testing.T) {
	t.Run("known TLS 1.2 cipher", func(t *testing.T) {
		id, err := OpenSSLCipherToGoID("ECDHE-RSA-AES128-GCM-SHA256")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 {
			t.Errorf("got 0x%04x, want 0x%04x", id, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)
		}
	})

	t.Run("all ciphers from intermediate profile resolve", func(t *testing.T) {
		profile := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
		for _, c := range profile.Ciphers {
			ianaNames := libgocrypto.OpenSSLToIANACipherSuites([]string{c})
			if len(ianaNames) == 0 {
				continue
			}
			_, err := OpenSSLCipherToGoID(c)
			if err != nil {
				t.Errorf("failed to resolve cipher %q: %v", c, err)
			}
		}
	})

	t.Run("unknown cipher returns error", func(t *testing.T) {
		_, err := OpenSSLCipherToGoID("BOGUS-CIPHER")
		if err == nil {
			t.Error("expected error for unknown cipher")
		}
	})
}

func TestTLSVersionBelow(t *testing.T) {
	tests := []struct {
		name     string
		input    uint16
		expected uint16
		wantErr  bool
	}{
		{name: "below TLS 1.3", input: tls.VersionTLS13, expected: tls.VersionTLS12},
		{name: "below TLS 1.2", input: tls.VersionTLS12, expected: tls.VersionTLS11},
		{name: "below TLS 1.1", input: tls.VersionTLS11, expected: tls.VersionTLS10},
		{name: "below TLS 1.0", input: tls.VersionTLS10, wantErr: true},
		{name: "unknown version", input: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TLSVersionBelow(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %d, got %d", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %d: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("TLSVersionBelow(%d) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}
