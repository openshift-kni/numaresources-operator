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

// flagcodec allows to manipulate foreign command lines following the
// standard golang conventions. It offeres two-way processing aka
// parsing/marshalling of flags. It is different from the other, well
// established packages (pflag...) because it aims to manipulate command
// lines in general, not this program command line.

package flagcodec

import (
	"fmt"
	"sort"
	"strings"
)

const (
	FlagToggle = iota
	FlagOption
)

type Val struct {
	Kind int
	Data string
}

type Flags struct {
	argv0 string
	args  map[string]Val
}

// ParseArgvKeyValue parses a clean (trimmed) argv whose components are
// either toggles or key=value pairs. IOW, this is a restricted and easier
// to parse flavour of argv on which option and value are guaranteed to
// be in the same item.
// IOW, we expect
// "--opt=foo"
// AND NOT
// "--opt", "foo"
func ParseArgvKeyValue(argv []string) *Flags {
	if len(argv) == 0 {
		return nil
	}
	// argv[0] is always expected to be the command name
	ret := &Flags{
		argv0: argv[0],
		args:  make(map[string]Val),
	}
	if len(argv) < 1 {
		return ret
	}
	ret.argv0 = argv[0]
	for _, arg := range argv[1:] {
		fields := strings.SplitN(arg, "=", 2)
		if len(fields) == 1 {
			ret.SetToggle(fields[0])
			continue
		}
		ret.SetOption(fields[0], fields[1])
	}
	return ret
}

func (fl *Flags) SetToggle(name string) {
	fl.args[name] = Val{
		Kind: FlagToggle,
	}
}

func (fl *Flags) SetOption(name, data string) {
	fl.args[name] = Val{
		Kind: FlagOption,
		Data: data,
	}
}

func (fl *Flags) Argv() []string {
	var argv []string
	for name, val := range fl.args {
		argv = append(argv, toString(name, val))
	}
	sort.Strings(argv)
	return append([]string{fl.argv0}, argv...)
}

func toString(name string, val Val) string {
	if val.Kind == FlagToggle {
		return name
	}
	return fmt.Sprintf("%s=%s", name, val.Data)
}
