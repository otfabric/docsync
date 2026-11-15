// SPDX-License-Identifier: MIT

//go:build unix

package repofs

import "syscall"

// openFlag keeps an open from waiting: a named pipe put in place of a file
// after its Stat opens at once and is then refused as not a regular file.
const openFlag = syscall.O_NONBLOCK
