// Command const answers every call with a response fixed at build time (-ldflags "-X main.reply=…").
package main

import (
	"fmt"
	"io"
	"os"
)

var reply = `{}`

func main() {
	_, _ = io.Copy(io.Discard, os.Stdin)
	fmt.Print(reply)
}
