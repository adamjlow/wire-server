// The existing chart invokes sh -c "sleep 10" before SIGTERM. This deliberately
// supports that exact lifecycle hook without installing a general shell.
package main

import (
	"fmt"
	"os"
	"time"
)

func hook(args []string, sleep func(time.Duration)) int {
	if len(args) != 2 || args[0] != "-c" || args[1] != "sleep 10" {
		return 2
	}
	sleep(10 * time.Second)
	return 0
}

func main() {
	code := hook(os.Args[1:], time.Sleep)
	if code != 0 {
		fmt.Fprintln(os.Stderr, "unsupported lifecycle command")
	}
	os.Exit(code)
}
