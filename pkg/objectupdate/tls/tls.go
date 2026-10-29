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
	"strconv"
	"strings"

	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

// Settings holds pre-computed string representations of TLS configuration.
type Settings struct {
	MinVersion       string
	CipherSuites     string
	CurvePreferences string // Comma-separated numeric IDs (e.g., "29,23,24")
}

func (s Settings) String() string {
	ret := fmt.Sprintf("MinVersion: %s, CipherSuites: %s", s.MinVersion, s.CipherSuites)
	if s.CurvePreferences != "" {
		return ret + fmt.Sprintf(", CurvePreferences: %s", s.CurvePreferences)
	}
	return ret
}

// NewSettings creates Settings from a tls.Config.
// NewTLSConfigFromProfile automatically applies Groups to CurvePreferences,
// so we just need to format the already-populated curve preferences as a string.
func NewSettings(cfg *tls.Config) Settings {
	cipherNames := make([]string, 0, len(cfg.CipherSuites))
	for _, id := range cfg.CipherSuites {
		cipherNames = append(cipherNames, tls.CipherSuiteName(id))
	}

	settings := Settings{
		MinVersion:   libgocrypto.TLSVersionToNameOrDie(cfg.MinVersion),
		CipherSuites: strings.Join(cipherNames, ","),
	}

	// cfg.CurvePreferences is already populated by NewTLSConfigFromProfile
	if len(cfg.CurvePreferences) > 0 {
		settings.CurvePreferences = curveIDsToString(cfg.CurvePreferences)
	}

	return settings
}

func curveIDsToString(ids []tls.CurveID) string {
	var sb strings.Builder
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(strconv.Itoa(int(id)))
	}
	return sb.String()
}
