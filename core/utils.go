// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"fmt"
	"os"
)

var debug = false

func init() {
	if os.Getenv("DEBUG") != "" {
		debug = true
	}
}

// Debugf is like fmt.Printf, but only output when we are in debug mode.
func Debugf(format string, args ...interface{}) {
	if debug {
		fmt.Printf(format, args...)
	}
}

// Debugln is like fmt.Println, but only output when we are in debug mode.
func Debugln(args ...interface{}) {
	if debug {
		fmt.Println(args)
	}
}
