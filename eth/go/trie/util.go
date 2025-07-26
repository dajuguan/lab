package trie

import "fmt"

var DEBUG = false

func DPrintf(format string, a ...interface{}) {
	if DEBUG {
		fmt.Printf(format+"\n", a...)
	}
}
