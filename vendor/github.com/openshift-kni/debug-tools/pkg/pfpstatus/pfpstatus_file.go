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

package pfpstatus

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/openshift-kni/debug-tools/pkg/pfpstatus/record"
)

func dumpLoop(ctx context.Context, env *environ, params StorageParams) {
	env.lh.V(4).Info("dump loop started")
	defer env.lh.V(4).Info("dump loop finished")
	ticker := time.NewTicker(params.Period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			env.mu.Lock()
			snapshot := env.rec.Content()
			env.mu.Unlock()

			var wg sync.WaitGroup
			for nodeName, statuses := range snapshot {
				wg.Add(1)
				go func(fileName string, statuses []record.RecordedStatus) {
					defer wg.Done()
					err := record.DumpToFile(params.Directory, fileName, statuses)
					if err != nil {
						env.lh.V(6).Error(err, "dumping file", "dir", params.Directory, "file", fileName, "statusCount", len(statuses))
					}
				}(record.NodeNameToFileName(nodeName), statuses)
			}
			wg.Wait()
		}
	}
}

func existsBaseDirectory(baseDir string) bool {
	info, err := os.Stat(baseDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}
