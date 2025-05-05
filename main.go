package main

import (
	"fmt"

	autosyncdoc "yrs-bindings-test/autoSyncDoc"
)

func main() {
	doc := autosyncdoc.NewAutoSyncDoc()
	defer doc.Destroy()

	err := doc.AddValue("message", "hello from Go!")
	if err != nil {
		fmt.Println("Error adding value:", err)
		return
	}

	jsonData, err := doc.ToJSON()
	if err != nil {
		fmt.Println("Error converting to JSON:", err)
		return
	}

	fmt.Printf("YDoc JSON: %+v\n", jsonData)
}
