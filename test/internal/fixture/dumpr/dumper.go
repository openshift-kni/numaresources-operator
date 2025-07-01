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

package dumpr

import (
	"bytes"
	"fmt"
	"io"
)

// Dumper sends multi-line output to the logging sink, bypassing the slog interface.
type Dumper interface {
	Infof(data string, format string, values ...any)
	Errorf(data string, err error, format string, values ...any)
}

type discarder struct{}

func (_ discarder) Infof(data string, format string, values ...any) {
}

func (_ discarder) Errorf(data string, err error, format string, values ...any) {
}

func Discard() Dumper {
	return discarder{}
}

type formatter struct {
	sink io.Writer
}

func (fr formatter) Infof(data string, format string, values ...any) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, format, values...)
	buf.WriteString("\n")
	buf.WriteString(data)
	buf.WriteString("\n")
	_, _ = fr.sink.Write(buf.Bytes())
}

func (fr formatter) Errorf(data string, err error, format string, values ...any) {
	var buf bytes.Buffer
	buf.WriteString(err.Error())
	buf.WriteString(": ")
	fmt.Fprintf(&buf, format, values...)
	buf.WriteString("\n")
	buf.WriteString(data)
	buf.WriteString("\n")
	_, _ = fr.sink.Write(buf.Bytes())
}

func NewFormatter(w io.Writer) Dumper {
	return formatter{
		sink: w,
	}
}
