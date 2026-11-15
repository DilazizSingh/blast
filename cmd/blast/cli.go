package main

import (
	"fmt"
	"io"
)

// name is the command's name wherever it is printed. Renaming the tool starts here.
const name = "blast"

func run(_ []string, stdout, _ io.Writer) int {
	fmt.Fprintf(stdout, "%s %s\n", name, version)
	return 0
}
