// Command kmx is the CLI entry point for Kaimahi's developer and governance
// workflows. Cobra owns syntax and help; internal/kmx/app owns all operations.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := execute(os.Args[1:], productionDependencies()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
