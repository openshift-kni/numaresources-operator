/*
 * Copyright 2022 Red Hat, Inc.
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

package remoteexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	utilportforward "k8s.io/apimachinery/pkg/util/portforward"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// CommandOnPod runs command in the pod and returns buffer output
func CommandOnPod(ctx context.Context, c kubernetes.Interface, pod *corev1.Pod, command ...string) ([]byte, []byte, error) {
	return CommandOnPodByNames(ctx, c, pod.Namespace, pod.Name, pod.Spec.Containers[0].Name, command...)
}

func CommandOnPodByNames(ctx context.Context, c kubernetes.Interface, podNamespace, podName, cntName string, command ...string) ([]byte, []byte, error) {
	var outputBuf bytes.Buffer
	var errorBuf bytes.Buffer

	req := c.CoreV1().RESTClient().
		Post().
		Namespace(podNamespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: cntName,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, nil, err
	}

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &outputBuf,
		Stderr: &errorBuf,
		Tty:    true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to run command %v: %w", command, err)
	}

	return outputBuf.Bytes(), errorBuf.Bytes(), nil
}

// PortForwardToPod opens an SPDY tunnel to the given pod port and calls fn
// with the resulting net.Conn that speaks directly to that port inside the pod.
// The data connection is closed after fn returns (before draining the error stream).
func PortForwardToPod(c kubernetes.Interface, pod *corev1.Pod, podPort string, fn func(conn net.Conn) error) error {
	return PortForwardToPodByNames(c, pod.Namespace, pod.Name, podPort, fn)
}

func PortForwardToPodByNames(c kubernetes.Interface, podNamespace, podName, podPort string, fn func(conn net.Conn) error) error {
	dialer, err := portForwardDialer(c, podNamespace, podName)
	if err != nil {
		return fmt.Errorf("port-forward dialer creation failed: %w", err)
	}

	spdyConn, _, err := dialer.Dial(utilportforward.PortForwardV1Name)
	if err != nil {
		return fmt.Errorf("port-forward dial to %s/%s:%s failed: %w", podNamespace, podName, podPort, err)
	}
	// by now the connection is established and the port-forward is running
	defer func() {
		if err := spdyConn.Close(); err != nil {
			klog.ErrorS(err, "failed to close SPDY connection")
		}
	}()

	// setup the error stream that would be used by kubelet to send back errors
	headers := http.Header{}
	headers.Set(corev1.StreamType, corev1.StreamTypeError)
	headers.Set(corev1.PortHeader, podPort)
	headers.Set(corev1.PortForwardRequestIDHeader, "0")
	errorStream, err := spdyConn.CreateStream(headers)
	if err != nil {
		return fmt.Errorf("port-forward error stream creation failed: %w", err)
	}
	defer func() {
		if err := errorStream.Close(); err != nil {
			klog.ErrorS(err, "failed to close error stream")
		}
	}()

	errorCh := make(chan error, 1)
	go func() {
		buf, err := io.ReadAll(errorStream)
		if err != nil {
			errorCh <- err
			return
		}
		if len(buf) > 0 {
			errorCh <- fmt.Errorf("port-forward to %s/%s:%s: %s", podNamespace, podName, podPort, string(buf))
			return
		}
		errorCh <- nil
	}()

	// setup the data stream
	headers.Set(corev1.StreamType, corev1.StreamTypeData)
	dataStream, err := spdyConn.CreateStream(headers)
	if err != nil {
		return fmt.Errorf("port-forward data stream creation failed: %w", err)
	}

	conn := &streamConn{stream: dataStream}
	fnErr := fn(conn)
	// Close the forwarded data stream as soon as fn finishes so the server can
	// wind down the port-forward and close the error stream. Otherwise io.ReadAll
	// on errorStream can block forever while we wait on errorCh below, and
	// deferred closes never run (deadlock).
	if err := conn.Close(); err != nil {
		klog.ErrorS(err, "failed to close port-forward data stream")
	}
	if fnErr != nil {
		return fnErr
	}
	if err := <-errorCh; err != nil {
		return err
	}
	return nil
}

func portForwardDialer(c kubernetes.Interface, podNamespace, podName string) (httpstream.Dialer, error) {
	req := c.CoreV1().RESTClient().
		Post().
		Namespace(podNamespace).
		Resource("pods").
		Name(podName).
		SubResource("portforward")

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())
	return dialer, nil
}

// streamConn wraps an httpstream.Stream interface as a net.Conn so it can be used with
// crypto/tls.Client and similar APIs that expect a net.Conn.
type streamConn struct {
	stream httpstream.Stream
}

func (s *streamConn) Read(b []byte) (int, error)  { return s.stream.Read(b) }
func (s *streamConn) Write(b []byte) (int, error) { return s.stream.Write(b) }
func (s *streamConn) Close() error                { return s.stream.Close() }

func (s *streamConn) LocalAddr() net.Addr              { return stubAddr{} }
func (s *streamConn) RemoteAddr() net.Addr             { return stubAddr{} }
func (s *streamConn) SetDeadline(time.Time) error      { return nil }
func (s *streamConn) SetReadDeadline(time.Time) error  { return nil }
func (s *streamConn) SetWriteDeadline(time.Time) error { return nil }

type stubAddr struct{}

func (stubAddr) Network() string { return "spdy" }
func (stubAddr) String() string  { return "port-forward" }
