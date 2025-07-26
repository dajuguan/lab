package main

import "fmt"

var DEBUG = false

func DPrintf(format string, a ...interface{}) {
	if DEBUG {
		fmt.Printf(format, a...)
	}
}

func DPrintln(a ...interface{}) {
	if DEBUG {
		fmt.Println(a...)
	}
}
