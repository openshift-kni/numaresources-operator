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
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-logr/logr"

	"github.com/k8stopologyawareschedwg/podfingerprint"

	"github.com/openshift-kni/debug-tools/pkg/pfpstatus/record"
)

const (
	PFPStatusDumpEnvVar string = "PFP_STATUS_DUMP"
)

const (
	DefaultDumpDirectory string = "/run/pfpstatus"
)

const (
	defaultMaxNodes          = 5000
	defaultMaxSamplesPerNode = 10
	defaultDumpPeriod        = 10 * time.Second
)

type StorageParams struct {
	Enabled   bool
	Directory string
	Period    time.Duration
}

type Params struct {
	Storage StorageParams
}

type environ struct {
	mu  sync.Mutex
	rec *record.Recorder
	lh  logr.Logger
}

func DefaultParams() Params {
	return Params{
		Storage: StorageParams{
			Enabled:   false,
			Directory: DefaultDumpDirectory,
			Period:    10 * time.Second,
		},
	}
}

func ParamsFromEnv(lh logr.Logger, params *Params) {
	dumpDir, ok := os.LookupEnv(PFPStatusDumpEnvVar)
	if !ok || dumpDir == "" {
		params.Storage.Enabled = false
	} else {
		params.Storage.Enabled = true
		params.Storage.Directory = dumpDir
	}

	// let's try to keep the amount of code we do in init() at minimum.
	// This may happen if the container didn't have the directory mounted
	if !existsBaseDirectory(dumpDir) {
		lh.Info("base directory not found, will discard everything", "baseDirectory", dumpDir)
		params.Storage.Enabled = false
	}
}

func Setup(logh logr.Logger, params Params) {
	if !params.Storage.Enabled {
		logh.Info("no backend enabled, nothing to do")
		return
	}

	logh.Info("Setup in progress", "params", fmt.Sprintf("%+#v", params))

	rec, err := record.NewRecorder(defaultMaxNodes, defaultMaxSamplesPerNode, time.Now)
	if err != nil {
		logh.Error(err, "cannot create a status recorder")
		return
	}

	ctx := context.Background()
	env := environ{
		rec: rec,
		lh:  logh,
	}

	ch := make(chan podfingerprint.Status)
	podfingerprint.SetCompletionSink(ch)
	go collectLoop(ctx, &env, ch)
	if params.Storage.Enabled {
		go dumpLoop(ctx, &env, params.Storage)
	}
}

func collectLoop(ctx context.Context, env *environ, updates <-chan podfingerprint.Status) {
	env.lh.V(4).Info("collect loop started")
	defer env.lh.V(4).Info("collect loop finished")
	for {
		select {
		case <-ctx.Done():
			return
		case st := <-updates:
			env.mu.Lock()
			_ = env.rec.Push(st) // intentionally ignore error
			env.mu.Unlock()
		}
	}
}
