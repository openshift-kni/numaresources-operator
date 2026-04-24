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
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
)

// ErrTLSHandshakeRejected is a base error for any TLS connection refusal.
var ErrTLSHandshakeRejected = errors.New("TLS handshake rejected")

// ErrTLSServerRejected indicates the remote server sent a TLS alert
// rejecting the handshake (e.g. unsupported version or cipher).
var ErrTLSServerRejected = fmt.Errorf("%w: server sent TLS alert", ErrTLSHandshakeRejected)

// ErrTLSClientConfigIncompatible indicates the Go TLS client refused to
// even attempt the connection because the configured parameters are
// incompatible (e.g. MinVersion > MaxVersion, or no matching cipher suites).
var ErrTLSClientConfigIncompatible = fmt.Errorf("%w: Go client TLS config incompatible", ErrTLSHandshakeRejected)

func wrapTLSHandshakeError(err error) error {
	if err == nil {
		return nil
	}
	var alertErr tls.AlertError
	if errors.As(err, &alertErr) {
		return fmt.Errorf("%w: %w", ErrTLSServerRejected, err)
	}
	if strings.Contains(err.Error(), "handshake failure") ||
		strings.Contains(err.Error(), "protocol version") {
		return fmt.Errorf("%w: %w", ErrTLSServerRejected, err)
	}
	if strings.Contains(err.Error(), "no supported versions") ||
		strings.Contains(err.Error(), "no ciphers available") {
		return fmt.Errorf("%w: %w", ErrTLSClientConfigIncompatible, err)
	}
	return err
}

func tlsHandshake(conn net.Conn, cfg *tls.Config) (*tls.ConnectionState, error) {
	tlsConn := tls.Client(conn, cfg)
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	defer func() {
		if err := tlsConn.Close(); err != nil {
			klog.ErrorS(err, "failed to close TLS connection")
		}
	}()
	state := tlsConn.ConnectionState()
	return &state, nil
}

// ProbeMaxTLSVersion checks whether the pod's server rejects
// TLS connections capped at the given maximum version.
func ProbeMaxTLSVersion(cli kubernetes.Interface, pod *corev1.Pod, podPort string, maxVersion uint16) error {
	key := client.ObjectKeyFromObject(pod)
	klog.InfoS("probe with max TLS version", "pod", key.String(), "port", podPort, "maxVersion", tls.VersionName(maxVersion))

	return remoteexec.PortForwardToPod(cli, pod, podPort, func(conn net.Conn) error {
		_, err := tlsHandshake(conn, &tls.Config{
			InsecureSkipVerify: true, // we care about the version/cipher not the cert chain so skip cert validation
			MinVersion:         maxVersion,
			MaxVersion:         maxVersion,
		})
		return wrapTLSHandshakeError(err)
	})
}

// ProbeTLSCipher checks whether the pod's server rejects
// TLS connections when the client only offers the given cipher (OpenSSL name).
// Since this is dedicated to invalidating ciphers, it is assumed that it is
// called on non TLS 1.3 configuration.
func ProbeTLSCipher(cli kubernetes.Interface, pod *corev1.Pod, podPort string, cipher string) error {
	cipherID, err := OpenSSLCipherToGoID(cipher)
	if err != nil {
		return err
	}
	key := client.ObjectKeyFromObject(pod)
	klog.InfoS("probe with TLS cipher", "pod", key.String(), "port", podPort, "cipher", cipher, "cipherID", cipherID)

	return remoteexec.PortForwardToPod(cli, pod, podPort, func(conn net.Conn) error {
		_, err := tlsHandshake(conn, &tls.Config{
			InsecureSkipVerify: true, // we care about the version/cipher not the cert chain so skip cert validation
			MaxVersion:         tls.VersionTLS12,
			CipherSuites:       []uint16{cipherID},
		})
		return wrapTLSHandshakeError(err)
	})
}

// ProbeTLSSettings opens a TLS connection to the pod and returns the negotiated
// TLS version and cipher suite.
func ProbeTLSSettings(cli kubernetes.Interface, pod *corev1.Pod, podPort string) (version uint16, cipherSuite uint16, err error) {
	key := client.ObjectKeyFromObject(pod)

	err = remoteexec.PortForwardToPod(cli, pod, podPort, func(conn net.Conn) error {
		state, tlsErr := tlsHandshake(conn, &tls.Config{
			InsecureSkipVerify: true, // we care about the version/cipher not the cert chain so skip cert validation
		})
		if tlsErr != nil {
			return tlsErr
		}
		version = state.Version
		cipherSuite = state.CipherSuite
		klog.InfoS("probe TLS settings", "pod", key.String(), "version", tls.VersionName(version), "cipher", tls.CipherSuiteName(cipherSuite))
		return nil
	})
	return version, cipherSuite, err
}

// OpenSSLCipherToGoID converts an OpenSSL cipher name to a Go crypto/tls
// cipher suite ID by going through the IANA name as an intermediate form.
func OpenSSLCipherToGoID(opensslName string) (uint16, error) {
	ianaNames := libgocrypto.OpenSSLToIANACipherSuites([]string{opensslName})
	if len(ianaNames) == 0 {
		return 0, fmt.Errorf("unknown OpenSSL cipher %q", opensslName)
	}
	ianaName := ianaNames[0]
	for _, cs := range tls.CipherSuites() {
		if cs.Name == ianaName {
			return cs.ID, nil
		}
	}
	for _, cs := range tls.InsecureCipherSuites() {
		if cs.Name == ianaName {
			return cs.ID, nil
		}
	}
	return 0, fmt.Errorf("no Go cipher suite for IANA name %q (from OpenSSL %q)", ianaName, opensslName)
}

// findDisallowedCipher returns the first TLS 1.2 cipher (OpenSSL name)
// from the broadest upstream profile (Old) that is not in the allowed set.
// TLS 1.3 ciphers are skipped because they are not individually configurable.
func FindDisallowedCipher(allowed []string) string {
	allowedSet := make(map[string]bool, len(allowed))
	for _, c := range allowed {
		allowedSet[c] = true
	}
	allCiphers := configv1.TLSProfiles[configv1.TLSProfileOldType].Ciphers
	for _, cipher := range allCiphers {
		if strings.HasPrefix(cipher, "TLS_") {
			continue
		}
		if !allowedSet[cipher] {
			return cipher
		}
	}
	return ""
}

func TLSVersionBelow(v uint16) (uint16, error) {
	switch v {
	case tls.VersionTLS13:
		return tls.VersionTLS12, nil
	case tls.VersionTLS12:
		return tls.VersionTLS11, nil
	case tls.VersionTLS11:
		return tls.VersionTLS10, nil
	default:
		return 0, fmt.Errorf("unknown TLS version %d", v)
	}
}
