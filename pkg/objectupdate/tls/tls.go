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
	"strings"

	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

// Settings holds pre-computed string representations of TLS configuration.
type Settings struct {
	MinVersion   string
	CipherSuites string
}

func (s Settings) String() string {
	return fmt.Sprintf("MinVersion: %s, CipherSuites: %s", s.MinVersion, s.CipherSuites)
}

// NewSettings creates Settings from a tls.Config by converting the
// numeric TLS version and cipher suite IDs to their string names.
func NewSettings(cfg *tls.Config) Settings {
	cipherNames := make([]string, 0, len(cfg.CipherSuites))
	for _, id := range cfg.CipherSuites {
		cipherNames = append(cipherNames, tls.CipherSuiteName(id))
	}
	return Settings{
		MinVersion:   libgocrypto.TLSVersionToNameOrDie(cfg.MinVersion),
		CipherSuites: strings.Join(cipherNames, ","),
	}
}
