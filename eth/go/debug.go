package main

import "fmt"

const DEBUG = false

func DPrintf(format string, a ...interface{}) {
	if DEBUG {
		fmt.Printf(format, a...)
	}
	return
}

func DPrintln(a ...interface{}) {
	if DEBUG {
		fmt.Println(a...)
	}
	return
}
