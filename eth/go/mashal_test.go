package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestUnma(t *testing.T) {
	var b Block
	s := `{
		"number":"0x1b4",
		"transactions":["0x5a2d3c7f8e4b6f8c9e4a5d3c7f8e4b6f8c9e4a5d3c7f8e4b6f8c9e4a5d3c7f8"]
		}`
	if err := json.Unmarshal([]byte(s), &b); err != nil {
		panic(err)
	}
	fmt.Println("b", b)
}
