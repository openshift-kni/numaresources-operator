/*
 * Copyright 2025 Red Hat, Inc.
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

package label

// The label package contains a list of labels that can be used in
// the e2e and serial tests to indicate and point certain behavior or characteristics
// of the various tests.
// Those can be filtered/focused by ginkgo before the test runs.

// Kind is a label indicates specific criteria that classify the test.
// A Mixture of kinds can be used for the same test
type Kind = string

const (
	// Slow means test that usually requires reboot or takes a long time to finish.
	Slow Kind = "slow"

	// ReleaseCritical is for tests that are critical for a successful release of the component that is being tested.
	ReleaseCritical Kind = "release_critical"

	// Reboot indicates that the test involves performing one or more reboots on one or more nodes.
	Reboot Kind = "reboot_required"
)

// Tier is a label to classify tests under specific grade/level
// that should roughly describe the execution complexity, maintainer identity and processing criteria.
type Tier = string

const (
	// Tier0 Critical Tests – Must Pass Always
	// Purpose: Ensures the most essential functionality of the system is working.
	// Scope: Covers critical user flows, system stability, and availability.
	// Execution: Bare minimum set of tests that has to be executed on every commit, PR, or build; failure blocks releases.
	Tier0 Tier = "tier0"

	// Tier1 High-Priority Tests – Core Features & Integrations
	// Purpose: Validates core feature functionality and key integrations.
	// Scope: Broader than Tier 0 but still crucial for business operations.
	Tier1 Tier = "tier1"

	// Tier2 Medium-Priority Tests – Extended Use Cases & Edge Scenarios
	// Purpose: Covers additional feature validations and complex user scenarios.
	// Scope: Tests that are important but don’t directly impact system uptime.
	Tier2 Tier = "tier2"

	// Tier3 Low-Priority Tests – Non-Critical and Exploratory
	// Purpose: Ensures non-essential features and edge cases function correctly.
	// Scope: Covers non regressions, slow tests, and rare conditions.
	Tier3 Tier = "tier3"
)

// Platform is a label that used to specify the type of platform on which
// tests are supposed to run
type Platform = string

const (
	// OpenShift means that tests are relevant only for OpenShift platform
	OpenShift Platform = "openshift"

	// HyperShift means that tests relevant only for HyperShift platform
	HyperShift Platform = "hypershift"
)
