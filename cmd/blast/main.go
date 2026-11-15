// Command blast shows which customers a pricing change hits before it ships.
package main

import "os"

// version is stamped at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
