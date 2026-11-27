// Command devkitd is the DevKit background daemon.
package main

import (
	"os"

	"github.com/hxx/hxxDiscourseDevKit/internal/cli"
)

func main() {
	os.Exit(cli.DaemonMain(os.Args[1:]))
}
