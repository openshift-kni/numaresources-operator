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
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
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
	t.Run("no supported elliptic curves for ECDHE", func(t *testing.T) {
		err := fmt.Errorf("tls: no supported elliptic curves for ECDHE")
		got := wrapTLSHandshakeError(err)
		if !errors.Is(got, ErrTLSClientConfigIncompatible) {
			t.Errorf("expected ErrTLSClientConfigIncompatible, got %v", got)
		}
		if !errors.Is(got, ErrTLSHandshakeRejected) {
			t.Errorf("expected ErrTLSHandshakeRejected in chain, got %v", got)
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

	t.Run("finds the one cipher missing from an otherwise complete profile", func(t *testing.T) {
		const missing = "ECDHE-ECDSA-AES256-SHA384"
		allowed := make([]string, 0, len(oldProfile.Ciphers))
		for _, c := range oldProfile.Ciphers {
			if c != missing {
				allowed = append(allowed, c)
			}
		}
		if got := FindDisallowedCipher(allowed); got != missing {
			t.Errorf("expected %q, got %q", missing, got)
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
			name: "stale cipher flag on TLS 1.3 profile",
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
			expectedError: "--metrics-tls-cipher-suites should be absent",
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
					"--metrics-tls-curve-preferences=4588,29,23,24",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:       "VersionTLS13",
				CipherSuites:     "",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: "",
		},
		{
			name: "multiple NodeGroups second has wrong curve flag",
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
					"--metrics-tls-curve-preferences=29,23,24",
				),
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns2",
					"rte-ds2",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--metrics-tls-curve-preferences=29,24,23",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23,24",
			},
			expectedError: `--metrics-tls-curve-preferences mismatch object="rte-ds2" expected="29,23,24" got="29,24,23"`,
		},
		{
			name: "CurvePreferences mismatch",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--metrics-tls-curve-preferences=29,24,23",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23,24",
			},
			expectedError: `--metrics-tls-curve-preferences mismatch object="rte-ds" expected="29,23,24" got="29,24,23"`,
		},
		{
			name: "no curves expected but stale curve flag present",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--metrics-tls-curve-preferences=29,23,24",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:   "VersionTLS12",
				CipherSuites: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			expectedError: "--metrics-tls-curve-preferences should be absent",
		},
		{
			name: "curves expected but flag absent",
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
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: `--metrics-tls-curve-preferences mismatch object="rte-ds" expected="4588,29,23,24" got=""`,
		},
		{
			name: "curves expected but flag empty",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--metrics-tls-curve-preferences=",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23",
			},
			expectedError: `--metrics-tls-curve-preferences mismatch object="rte-ds" expected="29,23" got=""`,
		},
		{
			name: "empty curve flag matches empty expectation",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--metrics-tls-curve-preferences=",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "",
			},
			expectedError: "",
		},
		{
			name: "single PQ curve 4588",
			nrop: newNROP(nropv1.NodeGroupStatus{
				DaemonSet: nropv1.NamespacedName{Namespace: "test-ns", Name: "rte-ds"},
			}),
			daemonSets: []client.Object{
				newRTEDaemonSetWithMainContainerArgs(
					"test-ns",
					"rte-ds",
					"--metrics-tls-min-version=VersionTLS12",
					"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--metrics-tls-curve-preferences=4588",
				),
			},
			expectedTLSSettings: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "4588",
			},
			expectedError: "",
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

func newSchedulerCR(deployment nropv1.NamespacedName) *nropv1.NUMAResourcesScheduler {
	return &nropv1.NUMAResourcesScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name: objectnames.DefaultNUMAResourcesSchedulerCrName,
		},
		Status: nropv1.NUMAResourcesSchedulerStatus{
			Deployment: deployment,
		},
	}
}

func newSchedulerDeployment(namespace, name string, containers ...corev1.Container) *appsv1.Deployment {
	if len(containers) == 0 {
		containers = []corev1.Container{
			{
				Name: schedupdate.MainContainerName,
			},
		}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "scheduler"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "scheduler"},
				},
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

func newSchedulerDeploymentWithMainContainerArgs(namespace, name string, args ...string) *appsv1.Deployment {
	return newSchedulerDeployment(namespace, name, corev1.Container{
		Name: schedupdate.MainContainerName,
		Args: args,
	})
}

func TestCheckSchedulerDeploymentTLSFlags(t *testing.T) {
	deploymentRef := nropv1.NamespacedName{Namespace: "numaresources-operator", Name: schedupdate.MainContainerName}
	testCases := []struct {
		name               string
		nroSched           *nropv1.NUMAResourcesScheduler
		objects            []client.Object
		expectedTLSSetting objtls.Settings
		expectedError      string
		notDeployed        bool
	}{
		{
			name:          "scheduler CR not found",
			nroSched:      nil,
			objects:       nil,
			expectedError: "scheduler is not deployed",
			notDeployed:   true,
		},
		{
			name:          "status deployment not reported",
			nroSched:      newSchedulerCR(nropv1.NamespacedName{}),
			expectedError: "is not reported",
		},
		{
			name:          "deployment not found",
			nroSched:      newSchedulerCR(deploymentRef),
			expectedError: "failed to get scheduler Deployment",
		},
		{
			name:     "scheduler container not found",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeployment(deploymentRef.Namespace, deploymentRef.Name,
					corev1.Container{Name: "kube-rbac-proxy"}),
			},
			expectedError: "main container not found",
		},
		{
			name:     "flags match intermediate profile",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
					"--tls-curve-preferences=4588,29,23,24",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: "",
		},
		{
			name:     "curve preferences order matters",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
					"--tls-curve-preferences=29,24,23",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23,24",
			},
			expectedError: `--tls-curve-preferences mismatch object="secondary-scheduler" expected="29,23,24" got="29,24,23"`,
		},
		{
			name:     "min version mismatch",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS13",
					"--tls-curve-preferences=4588,29,23,24",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: "mismatch",
		},
		{
			name:     "cipher suites mismatch",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
					"--tls-curve-preferences=4588,29,23,24",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: "mismatch",
		},
		{
			name:     "curves expected but flag absent",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23",
			},
			expectedError: `--tls-curve-preferences mismatch object="secondary-scheduler" expected="29,23" got=""`,
		},
		{
			name:     "curves expected but flag empty",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--tls-curve-preferences=",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "29,23",
			},
			expectedError: `--tls-curve-preferences mismatch object="secondary-scheduler" expected="29,23" got=""`,
		},
		{
			name:     "stale curve flag when empty expected",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--tls-curve-preferences=29,23",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "",
			},
			expectedError: "--tls-curve-preferences should be absent",
		},
		{
			name:     "no curve flag and empty expectation",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS12",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS12",
				CipherSuites:     "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				CurvePreferences: "",
			},
			expectedError: "",
		},
		{
			name:     "TLS 1.3 profile with fixed ciphers and curves",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS13",
					"--tls-curve-preferences=4588,29,23,24",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS13",
				CipherSuites:     "",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: "",
		},
		{
			name:     "stale cipher flag on TLS 1.3 profile",
			nroSched: newSchedulerCR(deploymentRef),
			objects: []client.Object{
				newSchedulerDeploymentWithMainContainerArgs(
					deploymentRef.Namespace,
					deploymentRef.Name,
					"--tls-min-version=VersionTLS13",
					"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"--tls-curve-preferences=4588,29,23,24",
				),
			},
			expectedTLSSetting: objtls.Settings{
				MinVersion:       "VersionTLS13",
				CipherSuites:     "",
				CurvePreferences: "4588,29,23,24",
			},
			expectedError: "--tls-cipher-suites should be absent",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objects := make([]client.Object, 0, len(tc.objects)+1)
			if tc.nroSched != nil {
				objects = append(objects, tc.nroSched)
			}
			objects = append(objects, tc.objects...)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objects...).Build()

			err := CheckSchedulerDeploymentTLSFlags(context.Background(), fakeClient, tc.expectedTLSSetting)

			if tc.notDeployed {
				if !errors.Is(err, ErrSchedulerNotDeployed) {
					t.Fatalf("expected ErrSchedulerNotDeployed, got %v", err)
				}
				return
			}
			if errors.Is(err, ErrSchedulerNotDeployed) {
				t.Fatalf("only a missing CR may report ErrSchedulerNotDeployed, got %v", err)
			}
			if tc.expectedError == "" {
				if err != nil {
					t.Errorf("no error expected to be returned; got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.expectedError)
			}
			if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("expected error %v, got %v", tc.expectedError, err)
			}
		})
	}
}

func TestCheckSchedulerDeploymentTLSFlagsNotDeployed(t *testing.T) {
	t.Run("missing CR reports ErrSchedulerNotDeployed", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()

		err := CheckSchedulerDeploymentTLSFlags(context.Background(), fakeClient, objtls.Settings{})
		if !errors.Is(err, ErrSchedulerNotDeployed) {
			t.Fatalf("expected ErrSchedulerNotDeployed, got %v", err)
		}
		if !strings.Contains(err.Error(), objectnames.DefaultNUMAResourcesSchedulerCrName) {
			t.Errorf("error should name the missing CR, got %v", err)
		}
	})

	t.Run("empty status stays retryable", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(newSchedulerCR(nropv1.NamespacedName{})).
			Build()

		err := CheckSchedulerDeploymentTLSFlags(context.Background(), fakeClient, objtls.Settings{})
		if err == nil {
			t.Fatal("expected an error for an unreconciled scheduler CR, got nil")
		}
		if errors.Is(err, ErrSchedulerNotDeployed) {
			t.Fatalf("an empty status must stay retryable, got ErrSchedulerNotDeployed: %v", err)
		}
	})
}

func TestFindDisallowedCurve(t *testing.T) {
	intermediate := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]

	t.Run("built-in profile returns the first curve the client can offer that is not allowed", func(t *testing.T) {
		allowed, unrecognized := CurveIDsFromProfile(*intermediate)
		if len(unrecognized) != 0 {
			t.Fatalf("built-in profile has unrecognized groups: %v", unrecognized)
		}
		got, err := FindDisallowedCurve(allowed)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != tls.CurveP521 {
			t.Errorf("expected CurveP521, got %v", got)
		}
	})
	t.Run("prefers CurveP521 over a post-quantum group", func(t *testing.T) {
		got, err := FindDisallowedCurve([]tls.CurveID{tls.X25519, tls.X25519MLKEM768})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != tls.CurveP521 {
			t.Errorf("expected CurveP521, got %v", got)
		}
	})
	t.Run("errors when every curve the client can offer is allowed", func(t *testing.T) {
		if _, err := FindDisallowedCurve(goClientCurves); err == nil {
			t.Errorf("expected an error, got nil")
		}
	})
}

func TestCurveIDsFromProfilee(t *testing.T) {
	t.Run("built-in intermediate profile maps every group in order", func(t *testing.T) {
		got, unrecognized := CurveIDsFromProfile(*configv1.TLSProfiles[configv1.TLSProfileIntermediateType])
		if len(unrecognized) != 0 {
			t.Fatalf("unexpected unrecognized groups: %v", unrecognized)
		}
		want := []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384}
		if len(got) != len(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("unexpected %v, got %v", want, got)
				break
			}
		}
	})
	t.Run("unknown group is reported and the known ones are kept", func(t *testing.T) {
		const unknown = configv1.TLSGroup("secp256k9")
		got, unrecognized := CurveIDsFromProfile(configv1.TLSProfileSpec{
			Groups: []configv1.TLSGroup{configv1.TLSGroupX25519, unknown},
		})
		if len(unrecognized) != 1 || unrecognized[0] != unknown {
			t.Errorf("expected [%q] as unrecognized, got %v", unknown, unrecognized)
		}
		if len(got) != 1 || got[0] != tls.X25519 {
			t.Errorf("expected [X25519] as mapped ids, got %v", got)
		}
	})
}

func TestTLSConfigOpts(t *testing.T) {
	t.Run("withCurve pins exactly one curve", func(t *testing.T) {
		cfg := &tls.Config{}
		withCurve(tls.CurveP384)(cfg)
		if len(cfg.CurvePreferences) != 1 || cfg.CurvePreferences[0] != tls.CurveP384 {
			t.Errorf("expected [CurveP384], got %v", cfg.CurvePreferences)
		}
	})

	t.Run("withCipher pins one suite and caps the version at TLS 1.2", func(t *testing.T) {
		cfg := &tls.Config{}
		withCipher(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256)(cfg)
		if len(cfg.CipherSuites) != 1 || cfg.CipherSuites[0] != tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 {
			t.Errorf("expected the single ECDHE-RSA-GCM suite, got %v", cfg.CipherSuites)
		}
		if cfg.MaxVersion != tls.VersionTLS12 {
			t.Errorf("expected MaxVersion TLS 1.2, got %s", tls.VersionName(cfg.MaxVersion))
		}
	})
}
