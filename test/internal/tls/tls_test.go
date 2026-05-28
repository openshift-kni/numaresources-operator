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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
)

func init() {
	utilruntime.Must(nropv1.AddToScheme(scheme.Scheme))
}

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

func newNROP(nodeGroups ...nropv1.NodeGroupStatus) *nropv1.NUMAResourcesOperator {
	return &nropv1.NUMAResourcesOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: objectnames.DefaultNUMAResourcesOperatorCrName,
		},
		Status: nropv1.NUMAResourcesOperatorStatus{
			NodeGroups: nodeGroups,
		},
	}
}

func newRTEDaemonSet(namespace, name string, containers ...corev1.Container) *appsv1.DaemonSet {
	if len(containers) == 0 {
		containers = []corev1.Container{
			{
				Name: rteupdate.MainContainerName,
			},
		}
	}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "rte"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "rte"},
				},
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

func newRTEDaemonSetWithMainContainerArgs(namespace, name string, args ...string) *appsv1.DaemonSet {
	return newRTEDaemonSet(namespace, name, corev1.Container{
		Name: rteupdate.MainContainerName,
		Args: args,
	})
}

func TestCheckRTEDaemonSetTLSFlags(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name                string
		nrop                *nropv1.NUMAResourcesOperator
		daemonSets          []client.Object
		expectedTLSSettings objtls.Settings
		expectedError       string
	}{
		{
			name:                "NROP not found",
			nrop:                nil,
			daemonSets:          nil,
			expectedTLSSettings: objtls.Settings{},
			expectedError:       "failed to get NUMAResourcesOperator",
		},
		{
			name:                "NROP has no NodeGroups",
			nrop:                newNROP(),
			daemonSets:          nil,
			expectedTLSSettings: objtls.Settings{},
			expectedError:       "at least one NodeGroup",
		},
		{
			name: "DaemonSet not found",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "missing-ds"},
			}),
			daemonSets:          nil,
			expectedTLSSettings: objtls.Settings{},
			expectedError:       "failed to get DaemonSet",
		},
		{
			name: "RTE container not found",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSet("test-ns", "rte-ds", corev1.Container{Name: "some-other-container"}),
			},
			expectedTLSSettings: objtls.Settings{},
			expectedError:       "main container not found",
		},
		{
			name: "flags match intermediate profile",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS12",
				CipherSuites: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			expectedError: "",
		},
		{
			name: "MinVersion mismatch",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS11",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS12",
				CipherSuites: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			expectedError: "mismatch",
		},
		{
			name: "multiple NodeGroups all match",
			nrop: newNROP(
				nropv1.NodeGroupStatus{
					DaemonSet: nropv1.NamespacedName{Namespace: "test-ns1", Name: "rte-ds1"},
				},
				nropv1.NodeGroupStatus{
					DaemonSet: nropv1.NamespacedName{Namespace: "test-ns2", Name: "rte-ds2"},
				},
			),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns1",
					"rte-ds1",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				),
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns2",
					"rte-ds2",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS12",
				CipherSuites: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			expectedError: "",
		},
		{
			name: "CipherSuite mismatch",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA256",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS12",
				CipherSuites: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			expectedError: "mismatch",
		},
		{
			name: "flags match modern (TLS 1.3) profile",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS13",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS13",
				CipherSuites: "",
			},
			expectedError: "",
		},
		{
			name: "cipher suites set for TLS 1.3 profile",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS13",
					"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS13",
				CipherSuites: "",
			},
			expectedError: "should be absent or empty",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objects := make([]client.Object, 0, len(tc.daemonSets)+1)
			if tc.nrop != nil {
				objects = append(objects, tc.nrop)
			}
			objects = append(objects, tc.daemonSets...)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objects...).Build()

			err := CheckRTEDaemonSetTLSFlags(ctx, fakeClient, tc.expectedTLSSettings)
			if err != nil {
				if tc.expectedError == "" {
					t.Errorf("no error expected to be returned; got %v", err)
				}
				if !strings.Contains(err.Error(), tc.expectedError) {
					t.Errorf("expected error %v, got %v", tc.expectedError, err)
				}
			} else if tc.expectedError != "" {
				t.Error("expected error, got nil")
			}
		})
	}
}
