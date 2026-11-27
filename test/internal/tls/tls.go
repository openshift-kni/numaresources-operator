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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	ctrltls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/flagcodec"
	k8swgobjupdate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/objectupdate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
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

// ErrSchedulerNotDeployed reports that NUMAResourcesScheduler is absent
// so there is no scheduler operand whose TLS settings can be verified
var ErrSchedulerNotDeployed = errors.New("scheduler is not deployed")

// tlsConfigOpt transforms client tls.Config
type tlsConfigOpt func(*tls.Config)

var goClientCurves = []tls.CurveID{
	tls.CurveP521,      // 25 - absent from every built-in profile
	tls.CurveP256,      // 23
	tls.CurveP384,      // 24
	tls.X25519,         // 29
	tls.X25519MLKEM768, // 4588 - listed by the built-in profiles since 5.1
}

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
		strings.Contains(err.Error(), "no ciphers available") ||
		strings.Contains(err.Error(), "no supported elliptic curves") {
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

// ProbeTLSState performs unconstrained handshake and returns full connection state
func ProbeTLSState(cli kubernetes.Interface, pod *corev1.Pod, port string) (tls.ConnectionState, error) {
	return probeTLSWithConfig(cli, pod, port)
}

// ProbeTLSCurve pins the handshake to a single curve (for positive or negative tests).
func ProbeTLSCurve(cli kubernetes.Interface, pod *corev1.Pod, port string, curve tls.CurveID) (tls.ConnectionState, error) {
	return probeTLSWithConfig(cli, pod, port, withCurve(curve))
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
	_, err = probeTLSWithConfig(cli, pod, podPort, withCipher(cipherID))
	return err
}

// OpenSSLCipherToGoID converts an OpenSSL cipher name to a Go crypto/tls
// cipher suite ID by going through the IANA name as an intermediate form.
// Both the secure and the insecure suite lists are searched, because a probe
// needs the ID of suites the profile deliberately excludes.
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

// FindDisallowedCipher returns the first TLS 1.2 cipher (OpenSSL name)
// from the broadest upstream profile (Old) that is not in the allowed set.
// TLS 1.3 ciphers are skipped because they are not individually configurable.
// allowed must hold the profile's cipher names verbatim, that is in OpenSSL
// form; IANA names never match and would make every cipher look disallowed
func FindDisallowedCipher(allowed []string) string {
	return FindDisallowedCipherForKey(allowed, "")
}

// FindDisallowedCipherForKey is FindDisallowedCipher restricted to the ciphers a
// server presenting a certificate of the given key type ("RSA", "ECDSA",
// "Ed25519") could actually select. A suite whose key exchange or signature
// algorithm does not match the certificate is rejected before the profile is
// consulted, so probing it would prove nothing about the profile. An empty
// keyType disables the filter.
func FindDisallowedCipherForKey(allowed []string, keyType string) string {
	allowedSet := make(map[string]bool, len(allowed))
	for _, c := range allowed {
		allowedSet[c] = true
	}
	allCiphers := configv1.TLSProfiles[configv1.TLSProfileOldType].Ciphers
	for _, cipher := range allCiphers {
		if strings.HasPrefix(cipher, "TLS_") {
			continue
		}
		if allowedSet[cipher] {
			continue
		}
		if keyType != "" && !strings.Contains(cipher, "-"+keyType+"-") {
			continue
		}
		return cipher
	}
	return ""
}

// ServerCertificateKeyType returns the public key algorithm of the certificate
// the endpoint presented. ProbeTLSState records the peer certificates even
// though the probe skips verification, so this costs no extra handshake.
func ServerCertificateKeyType(state tls.ConnectionState) string {
	if len(state.PeerCertificates) == 0 {
		return ""
	}
	switch state.PeerCertificates[0].PublicKey.(type) {
	case *rsa.PublicKey:
		return "RSA"
	case *ecdsa.PublicKey:
		return "ECDSA"
	case ed25519.PublicKey:
		return "Ed25519"
	}
	return ""
}

// GetClusterTLSProfile returns the cluster APIServer TLS profile spec and its minimum TLS version.
func GetClusterTLSProfile(ctx context.Context, cli client.Client) (minVersion uint16, profileSpec configv1.TLSProfileSpec, err error) {
	profileSpec, err = ctrltls.FetchAPIServerTLSProfile(ctx, cli)
	if err != nil {
		return 0, configv1.TLSProfileSpec{}, fmt.Errorf("unable to fetch TLS profile from API Server: %w", err)
	}
	cfg := tlsConfigFromProfileSpec(profileSpec)
	klog.InfoS("cluster TLS minimum version", "version", tls.VersionName(cfg.MinVersion))
	return cfg.MinVersion, profileSpec, nil
}

// RTESettingsFromClusterProfile returns expected RTE metrics TLS flags for the cluster APIServer profile.
func RTESettingsFromClusterProfile(ctx context.Context, cli client.Client) (objtls.Settings, error) {
	_, profileSpec, err := GetClusterTLSProfile(ctx, cli)
	if err != nil {
		return objtls.Settings{}, err
	}
	return objtls.NewSettings(tlsConfigFromProfileSpec(profileSpec)), nil
}

// CheckRTEDaemonSetTLSFlagsMatchClusterProfile verifies RTE DaemonSet TLS flags against the cluster APIServer profile.
func CheckRTEDaemonSetTLSFlagsMatchClusterProfile(ctx context.Context, cli client.Client) error {
	settings, err := RTESettingsFromClusterProfile(ctx, cli)
	if err != nil {
		return err
	}
	return CheckRTEDaemonSetTLSFlags(ctx, cli, settings)
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

func CheckRTEDaemonSetTLSFlags(ctx context.Context, cli client.Client, expected objtls.Settings) error {
	nropObj := &nropv1.NUMAResourcesOperator{}
	if err := cli.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, nropObj); err != nil {
		return fmt.Errorf("failed to get NUMAResourcesOperator: %w", err)
	}
	if len(nropObj.Status.NodeGroups) == 0 {
		return fmt.Errorf("expect the numaresourcesoperator to have at least one NodeGroup in status")
	}
	for _, ng := range nropObj.Status.NodeGroups {
		ds := &appsv1.DaemonSet{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: ng.DaemonSet.Namespace, Name: ng.DaemonSet.Name}, ds); err != nil {
			return fmt.Errorf("failed to get DaemonSet %s/%s: %w", ng.DaemonSet.Namespace, ng.DaemonSet.Name, err)
		}
		rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, rteupdate.MainContainerName)
		if rteCnt == nil {
			return fmt.Errorf("main container not found daemonsetName=%q", ds.Name)
		}
		rteFlags := flagcodec.ParseArgvKeyValue(rteCnt.Args, flagcodec.WithFlagNormalization)
		if err := matchTLSFlag(rteFlags, "--metrics-tls-min-version", expected.MinVersion, ds.Name); err != nil {
			return err
		}
		// TLS 1.3 cipher suites are fixed by the Go crypto/tls implementation and cannot
		// be configured, so expected.CipherSuites will be empty for the Modern profile.
		if err := matchTLSFlag(rteFlags, "--metrics-tls-cipher-suites", expected.CipherSuites, ds.Name); err != nil {
			return err
		}
		if err := matchTLSFlag(rteFlags, "--metrics-tls-curve-preferences", expected.CurvePreferences, ds.Name); err != nil {
			return err
		}
	}
	return nil
}

// FetchMetrics performs TLS handshake to rte pods metrics port and issues an HTTP Get /metrics request,
// returning the response body
func FetchMetrics(cli kubernetes.Interface, pod *corev1.Pod, podPort string) ([]byte, error) {
	var body []byte
	err := remoteexec.PortForwardToPod(cli, pod, podPort, func(conn net.Conn) error {
		var reqErr error
		body, reqErr = doMetricsRequestOverConnection(conn, podPort)
		return reqErr
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}

// CheckSchedulerDeploymentTLSFlags Resolves the scheduler Deployment from the
// NUMAResourcesScheduler status and compares --tls-min-version, --tls-cipher-suites, --tls-curve-preferences
func CheckSchedulerDeploymentTLSFlags(ctx context.Context, cli client.Client, expected objtls.Settings) error {
	nroSched := &nropv1.NUMAResourcesScheduler{}
	schedKey := client.ObjectKey{Name: objectnames.DefaultNUMAResourcesSchedulerCrName}
	// fetch CR from cluster
	if err := cli.Get(ctx, schedKey, nroSched); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%w: NUMAResourcesScheduler %q not found", ErrSchedulerNotDeployed, schedKey.Name)
		}
		return fmt.Errorf("failed to get NUMAResourcesScheduler: %w", err)
	}

	// Read the deployment reference that NROP scheduler controller publishes in status
	depRef := nroSched.Status.Deployment
	if depRef.Namespace == "" || depRef.Name == "" {
		return fmt.Errorf("Scheduler deployment is not reported in NUMAResourcesScheduler Status %q status", schedKey.Name)
	}
	// fetch scheduler deployer
	dp := &appsv1.Deployment{}
	if err := cli.Get(ctx, client.ObjectKey{Namespace: depRef.Namespace, Name: depRef.Name}, dp); err != nil {
		return fmt.Errorf("failed to get scheduler Deployment %s/%s: %w", depRef.Namespace, depRef.Name, err)
	}
	// searches Deployments pod template for the scheduler container
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, schedupdate.MainContainerName)

	if cnt == nil {
		return fmt.Errorf("main container not found deploymentName: %q", dp.Name)
	}
	// parse arguments of the form --flag=value
	flags := flagcodec.ParseArgvKeyValue(cnt.Args, flagcodec.WithFlagNormalization)
	if err := matchTLSFlag(flags, "--tls-min-version", expected.MinVersion, dp.Name); err != nil {
		return err
	}
	if err := matchTLSFlag(flags, "--tls-cipher-suites", expected.CipherSuites, dp.Name); err != nil {
		return err
	}

	if err := matchTLSFlag(flags, "--tls-curve-preferences", expected.CurvePreferences, dp.Name); err != nil {
		return err
	}
	return nil
}

// CurveIDsFromProfile returns the curve IDs the profile allows, plus the groups this build of Go cannot map
func CurveIDsFromProfile(profileSpec configv1.TLSProfileSpec) ([]tls.CurveID, []configv1.TLSGroup) {
	return libgocrypto.TLSGroupsToCurveIDs(profileSpec.Groups)
}

// FindDisallowedCurve return disallowed CurveID not supported by openshift
func FindDisallowedCurve(allowed []tls.CurveID) (tls.CurveID, error) {
	allowedSet := make(map[tls.CurveID]bool, len(allowed))
	for _, id := range allowed {
		allowedSet[id] = true
	}
	for _, id := range goClientCurves {
		if !allowedSet[id] {
			return id, nil
		}
	}
	return 0, fmt.Errorf("every curve the Go client can offer is allowed: %v", allowed)
}

// withCurve pins the client to offer only the specified key-exchange curve
func withCurve(curve tls.CurveID) tlsConfigOpt {
	return func(cfg *tls.Config) {
		cfg.CurvePreferences = []tls.CurveID{curve}
	}
}

// withCipher pins the client to offer only a specific cipher suite
func withCipher(cipher uint16) tlsConfigOpt {
	return func(cfg *tls.Config) {
		cfg.MaxVersion = tls.VersionTLS12
		cfg.CipherSuites = []uint16{cipher}
	}
}

// ProbeTLSWithConfig handles TLS handshake over port-forwarded connection
func probeTLSWithConfig(cli kubernetes.Interface, pod *corev1.Pod, port string, opts ...tlsConfigOpt) (tls.ConnectionState, error) {
	var state *tls.ConnectionState
	// Default baseline client configuration
	cfg := &tls.Config{
		InsecureSkipVerify: true, // Probing protocol policy, not certificate authority
	}

	for _, opt := range opts {
		opt(cfg)
	}
	err := remoteexec.PortForwardToPod(cli, pod, port, func(conn net.Conn) error {
		var handshakeErr error
		state, handshakeErr = tlsHandshake(conn, cfg)
		return wrapTLSHandshakeError(handshakeErr)
	})

	if state == nil {
		return tls.ConnectionState{}, err
	}
	return *state, err
}

func doMetricsRequestOverConnection(conn net.Conn, podPort string) ([]byte, error) {
	httpCli := buildMetricsHTTPClient(conn)
	defer httpCli.CloseIdleConnections()
	return executeMetricsRequest(httpCli, podPort)
}
func buildMetricsHTTPClient(conn net.Conn) *http.Client {
	usedConn := false
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		// The port-forward callback provides a single live stream connection
		// we allow exactly one dial so HTTP/TLS uses this stream and fails fast
		// if a retry/exta dial is attempted
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			if usedConn {
				return nil, fmt.Errorf("unexpected extra dial attempt while fetching metrics")
			}
			usedConn = true
			return conn, nil
		},
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: tr}
}

func executeMetricsRequest(httpCli *http.Client, podPort string) ([]byte, error) {
	const url = "https://127.0.0.1/metrics"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}
	req.Host = "127.0.0.1:" + podPort

	resp, err := httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed HTTPS Get /metrics: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.ErrorS(err, "failed to close /metrics response body")
		}
	}()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read /metrics response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected /metrics status code %d: %q", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

// matchTLSFlag compares TLS Flags rendered value on a workload against the expected value,
// and fails if flag is missing, stale or different
func matchTLSFlag(flags *flagcodec.Flags, flagName, expected, objectRef string) error {
	val, found := flags.GetFlag(flagName)
	if expected == "" {
		if found && val.Data != "" {
			return fmt.Errorf("%s should be absent or empty for this TLS profile object=%q got=%q", flagName, objectRef, val.Data)
		}
		return nil
	}
	if !found || val.Data != expected {
		got := ""
		if found {
			got = val.Data
		}
		return fmt.Errorf("%s mismatch object=%q expected=%q got=%q", flagName, objectRef, expected, got)
	}
	return nil
}

func tlsConfigFromProfileSpec(profileSpec configv1.TLSProfileSpec) *tls.Config {
	// unsupported holds cipher names and group names the shared library could not
	// translate; the operands drop the same entries, so this is informational.
	tlsConfigFn, unsupported := ctrltls.NewTLSConfigFromProfile(profileSpec)
	if len(unsupported) > 0 {
		klog.InfoS("TLS profile has unsupported entries", "ciphers", unsupported)
	}
	cfg := &tls.Config{}
	tlsConfigFn(cfg)
	return cfg
}
