// SPDX-License-Identifier: MIT
//
// Copyright © 2020 Kent Gibson <warthog618@gmail.com>.

// +build linux

package serial

var defaultConfig = Config{
	port: "/dev/ttyUSB0",
	baud: 115200,
}
