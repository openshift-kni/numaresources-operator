/*
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

package config

import (
	"os"
	"testing"

	"k8s.io/klog/v2"
)

func TestWatchRewrite(t *testing.T) {
	initialPath, err := writeTempFile("initial content")
	if err != nil {
		t.Fatalf("cannot set initial content: %v", err)
	}
	modifiedPath, err := writeTempFile("modified content")
	if err != nil {
		t.Fatalf("cannot set modified content: %v", err)
	}

	changeChan := make(chan bool)
	cw, err := NewWatcher(initialPath, func() error {
		changeChan <- true
		return nil
	})
	if err != nil {
		t.Fatalf("cannot watch %q: %v", initialPath, err)
	}
	go cw.WaitUntilChanges()

	err = os.Rename(modifiedPath, initialPath)
	if err != nil {
		t.Fatalf("rename %q -> %q failed: %v", modifiedPath, initialPath, err)
	}
	klog.Infof("%q -> %q", modifiedPath, initialPath)

	changed := <-changeChan
	if !changed {
		t.Fatalf("failed to detect change (rename)")
	}
}

func TestWatchWritesInPlace(t *testing.T) {
	f, err := os.CreateTemp("", "testwatchconf")
	if err != nil {
		t.Fatalf("CreateTemp failed: %v", err)
	}
	defer os.Remove(f.Name()) // clean up

	changeChan := make(chan bool)
	cw, err := NewWatcher(f.Name(), func() error {
		changeChan <- true
		return nil
	})
	if err != nil {
		t.Fatalf("cannot watch %q: %v", f.Name(), err)
	}
	go cw.WaitUntilChanges()

	if _, err := f.Write([]byte("content")); err != nil {
		t.Fatalf("Write %q failed: %v", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close %q failed: %v", f.Name(), err)
	}

	changed := <-changeChan
	if !changed {
		t.Fatalf("failed to detect change (write)")
	}
}

func writeTempFile(content string) (string, error) {
	f, err := os.CreateTemp("", "testwatchconf")
	if err != nil {
		return "", err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
