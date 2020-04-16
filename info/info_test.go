// SPDX-License-Identifier: MIT
//
// Copyright © 2018 Kent Gibson <warthog618@gmail.com>.

package info_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/warthog618/modem/info"
)

func TestHasPrefix(t *testing.T) {
	l := "cmd: blah"
	assert.True(t, info.HasPrefix(l, "cmd"))
	assert.False(t, info.HasPrefix(l, "cmd:"))
}

func TestTrimPrefix(t *testing.T) {
	// no prefix
	i := info.TrimPrefix("info line", "cmd")
	assert.Equal(t, "info line", i)

	// prefix
	i = info.TrimPrefix("cmd:info line", "cmd")
	assert.Equal(t, "info line", i)

	// prefix and space
	i = info.TrimPrefix("cmd: info line", "cmd")
	assert.Equal(t, "info line", i)
}
