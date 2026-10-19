// Command modes is the test fixture: one WASI program whose behaviour is picked by the name it is run under
// (os.Args[0] is the hook file name, e.g. "echo.wasm").
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var counter int

func main() {
	mode := strings.TrimSuffix(filepath.Base(os.Args[0]), ".wasm")
	raw, _ := io.ReadAll(os.Stdin)
	var req struct {
		Event string         `json:"event"`
		Item  map[string]any `json:"item"`
		Doc   map[string]any `json:"doc"`
	}
	_ = json.Unmarshal(raw, &req)
	note := func(s string) { fmt.Printf(`{"set":{"notes":%q}}`, s) }
	switch mode {
	case "echo":
		note(string(raw))
	case "set":
		fmt.Print(`{"set":{"readingMinutes":4,"score":2.5,"subtitle":null,"tags":["a","b"]}}`)
	case "setfloat":
		fmt.Print(`{"set":{"readingMinutes":5.0}}`)
	case "setbad":
		fmt.Print(`{"set":{"readingMinutes":"many"}}`)
	case "setunknown":
		fmt.Print(`{"set":{"nope":1}}`)
	case "validate":
		fmt.Print(`{"errors":[{"path":"title","message":"Too long"},{"path":"notes","code":"custom","message":"Second"}]}`)
	case "block":
		fmt.Print(`{"error":"Publishing is frozen"}`)
	case "badkey":
		fmt.Print(`{"foo":1}`)
	case "badjson":
		fmt.Print(`not json`)
	case "empty":
	case "emptyobj":
		fmt.Print(`{}`)
	case "loop":
		for {
		}
	case "sleep":
		time.Sleep(150 * time.Millisecond)
		fmt.Print(`{}`)
	case "sleeplong":
		time.Sleep(30 * time.Second)
		fmt.Print(`{}`)
	case "blockempty":
		fmt.Print(`{"error":""}`)
	case "grow":
		var keep [][]byte
		for i := 0; i < 1024; i++ {
			b := make([]byte, 16<<20)
			for j := range b {
				b[j] = 1
			}
			keep = append(keep, b)
		}
		fmt.Print(len(keep))
	case "open":
		_, err := os.Open("/etc/passwd")
		_, err2 := os.ReadDir("/")
		note(fmt.Sprintf("open: %v | readdir: %v", err, err2))
	case "dial":
		_, err := net.Dial("tcp", "127.0.0.1:80")
		note(fmt.Sprintf("dial: %v", err))
	case "exit1":
		fmt.Fprint(os.Stderr, "boom: something broke")
		os.Exit(1)
	case "panic":
		panic("fixture panic")
	case "huge":
		os.Stdout.Write([]byte(strings.Repeat("x", 2<<20)))
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("e", 1<<20))
		fmt.Print(`{}`)
	case "counter":
		counter++
		note(fmt.Sprint(counter))
	case "env":
		note(fmt.Sprintf("env=%d args=%v", len(os.Environ()), os.Args))
	case "clock":
		note(fmt.Sprint(time.Now().Year() >= 2024))
	case "reading":
		words := 0
		if s, ok := req.Doc["body"].(string); ok {
			words = len(strings.Fields(s))
		}
		fmt.Printf(`{"set":{"readingMinutes":%d}}`, max(1, words/200))
	case "noshout":
		if s, ok := req.Doc["title"].(string); ok && s == strings.ToUpper(s) && len(s) > 3 {
			fmt.Print(`{"errors":[{"path":"title","message":"No shouting"}]}`)
		}
	case "freeze":
		if s, ok := req.Doc["title"].(string); ok && strings.HasPrefix(s, "FROZEN") {
			fmt.Print(`{"error":"Publishing is frozen"}`)
		}
	case "notify":
		fmt.Fprintf(os.Stderr, "notified %s %v", req.Event, req.Item["id"])
		fmt.Print(`{}`)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode", mode)
		os.Exit(3)
	}
}
