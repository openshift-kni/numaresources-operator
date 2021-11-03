/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package tlog

import (
	"io/ioutil"
	"log"
)

type Logger interface {
	Printf(format string, v ...interface{})
	Debugf(format string, v ...interface{})
}

type LogAdapter struct {
	log      *log.Logger
	debugLog *log.Logger
}

func NewLogAdapter(log, debugLog *log.Logger) LogAdapter {
	return LogAdapter{
		log:      log,
		debugLog: debugLog,
	}
}

func NewNullLogAdapter() LogAdapter {
	nullLog := log.New(ioutil.Discard, "", 0)
	return NewLogAdapter(nullLog, nullLog)
}

func (la LogAdapter) Printf(format string, v ...interface{}) {
	la.log.Printf(format, v...)
}

func (la LogAdapter) Debugf(format string, v ...interface{}) {
	la.debugLog.Printf(format, v...)
}
