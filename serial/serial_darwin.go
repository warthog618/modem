// SPDX-License-Identifier: MIT
//
// Copyright © 2020 Kent Gibson <warthog618@gmail.com>.

// +build darwin

package serial

var defaultConfig = Config{
	port: "/dev/tty.usbserial",
	baud: 115200,
}
