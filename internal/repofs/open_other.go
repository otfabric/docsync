// SPDX-License-Identifier: MIT

//go:build !unix

package repofs

// openFlag adds nothing where an open of a file does not wait for a peer.
const openFlag = 0
