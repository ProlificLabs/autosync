package main

import (
	"fmt"

	"yrs-bindings-test/yrs"
)

func main() {
	doc := yrs.NewDoc()
	defer doc.Free()

	fmt.Println(doc.Hello())
}
