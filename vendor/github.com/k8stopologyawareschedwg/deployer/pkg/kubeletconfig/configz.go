/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubeletconfig

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-logr/logr"

	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
)

func GetKubeletConfigForNodes(kc *Kubectl, nodeNames []string, logger logr.Logger) (map[string]*kubeletconfigv1beta1.KubeletConfiguration, error) {
	cmd := kc.Command("proxy", "-p", "0")
	stdout, stderr, err := StartWithStreamOutput(cmd)
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	defer stderr.Close()
	defer cmd.Process.Kill()

	port, err := FindProxyPort(stdout)
	if err != nil {
		return nil, err
	}
	logger.Info("using proxy", "port", port)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	kubeletConfs := make(map[string]*kubeletconfigv1beta1.KubeletConfiguration)
	for _, nodeName := range nodeNames {
		endpoint := fmt.Sprintf("http://127.0.0.1:%d/api/v1/nodes/%s/proxy/configz", port, nodeName)

		logger.Info("requesting to proxy", "endpoint", endpoint)
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			logger.Info("request creation failed - skipped", "endpoint", endpoint, "error", err)
			continue
		}
		req.Header.Add("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			logger.Info("request creation failed - skipped", "endpoint", endpoint, "error", err)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			logger.Info("unexpected response status code - skipped", "endpoint", endpoint, "statusCode", resp.StatusCode)
			continue
		}

		conf, err := decodeConfigz(resp)
		if err != nil {
			logger.Info("response decode failed - skipped", "endpoint", endpoint, "error", err)
			continue
		}

		kubeletConfs[nodeName] = conf
	}
	return kubeletConfs, nil
}

func FindProxyPort(r io.Reader) (int, error) {
	buf := make([]byte, 128)
	n, err := r.Read(buf)
	if err != nil {
		return -1, err
	}
	output := string(buf[:n])
	proxyRegexp, err := regexp.Compile("Starting to serve on 127.0.0.1:([0-9]+)")
	if err != nil {
		return -1, err
	}
	match := proxyRegexp.FindStringSubmatch(output)
	if match == nil {
		return -1, fmt.Errorf("cannot find port announcement")
	}
	return strconv.Atoi(match[1])
}

func decodeConfigz(resp *http.Response) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	type configzWrapper struct {
		ComponentConfig kubeletconfigv1beta1.KubeletConfiguration `json:"kubeletconfig"`
	}

	configz := configzWrapper{}
	contentsBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(contentsBytes, &configz)
	if err != nil {
		return nil, err
	}

	return &configz.ComponentConfig, nil
}
