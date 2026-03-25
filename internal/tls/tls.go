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
	"fmt"

	"k8s.io/klog/v2"
)

func ValidateConfig(tlsConfig func(*tls.Config), unsupportedCiphers []string) error {
	if len(unsupportedCiphers) > 0 {
		klog.InfoS("TLS profile configuration contains unsupported ciphers that will be ignored", "unsupported", unsupportedCiphers)
	}

	if tlsConfig == nil {
		// must not happen
		return fmt.Errorf("TLS config function is nil")
	}

	testTLSConfig := &tls.Config{}
	tlsConfig(testTLSConfig)
	klog.InfoS("validate TLS Config", "minVersion", testTLSConfig.MinVersion, "cipherSuites", testTLSConfig.CipherSuites)
	if testTLSConfig.MinVersion == 0 || len(testTLSConfig.CipherSuites) == 0 { // must not happen
		return fmt.Errorf("invalid TLS config version or cipher suites")
	}

	minVersionStr := VersionToString(testTLSConfig.MinVersion)
	if minVersionStr == "" {
		return fmt.Errorf("unsupported TLS version %q", testTLSConfig.MinVersion)
	}

	return nil
}

func CipherSuitesToString(ciphers []uint16) []string {
	names := make([]string, 0, len(ciphers))
	for _, id := range ciphers {
		names = append(names, tls.CipherSuiteName(id))
	}
	return names
}

func VersionToString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "VersionTLS10"
	case tls.VersionTLS11:
		return "VersionTLS11"
	case tls.VersionTLS12:
		return "VersionTLS12"
	case tls.VersionTLS13:
		return "VersionTLS13"
	default:
		return ""
	}
}
