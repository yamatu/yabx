// Go 1.24+ enables MPTCP on listeners by default, which makes CVE-2026-98432
// remotely exploitable; explicit tcpMptcp / tcp_multi_path still opts in.
//go:debug multipathtcp=0
package main

import (
	"github.com/InazumaV/V2bX/cmd"
)

func main() {
	cmd.Run()
}
