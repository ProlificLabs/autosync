package main

import (
	"fmt"

	autosyncdoc "yrs-bindings-test/autoSyncDoc"
)

func main() {
	doc := autosyncdoc.NewAutoSyncDoc()
	defer doc.Destroy()

	// Get and print initial state
	initialState, err := doc.ToJSON()
	if err != nil {
		fmt.Println("Error getting initial JSON:", err)
		return
	}
	fmt.Printf("Initial YDoc JSON: %+v\n", initialState)

	// Define the desired new state
	newState := map[string]interface{}{
		"message": "hello from UpdateToState!",
		"count":   10,
		"nested": map[string]interface{}{
			"value": true,
		},
		"items": []interface{}{"a", "b", 3},
	}
	fmt.Printf("Target State: %+v\n", newState)

	// Update the document to the new state
	patch, err := autosyncdoc.UpdateToState(doc, newState)
	if err != nil {
		fmt.Println("Error calling UpdateToState:", err)
		return
	}
	fmt.Printf("Generated Patch: %+v\n", patch.List())

	// Get and print the modified state
	modifiedState, err := doc.ToJSON()
	if err != nil {
		fmt.Println("Error getting modified JSON:", err)
		return
	}
	fmt.Printf("Modified YDoc JSON after UpdateToState: %+v\n", modifiedState)

	firstStateVector, err := doc.GetStateVector()
	if err != nil {
		fmt.Println("Error getting first state vector:", err)
		return
	}

	emptyState := make(map[string]interface{})
	patch, err = autosyncdoc.UpdateToState(doc, emptyState)
	if err != nil {
		fmt.Println("Error calling UpdateToState:", err)
		return
	}
	fmt.Printf("Generated Patch: %+v\n", patch.List())

	secondStateVector, err := doc.GetStateVector()
	if err != nil {
		fmt.Println("Error getting second state vector:", err)
		return
	}

	// Get and print the final state
	finalState, err := doc.ToJSON()
	if err != nil {
		fmt.Println("Error getting final JSON:", err)
		return
	}
	fmt.Printf("Final YDoc JSON after UpdateToState: %+v\n", finalState)

	err = doc.ApplyStateVector(firstStateVector)
	if err != nil {
		fmt.Println("Error applying first state vector:", err)
		return
	}

	firstSVState, err := doc.ToJSON()
	if err != nil {
		fmt.Println("Error getting first state vector JSON:", err)
		return
	}
	fmt.Printf("First State Vector JSON: %+v\n", firstSVState)

	err = doc.ApplyStateVector(secondStateVector)
	if err != nil {
		fmt.Println("Error applying second state vector:", err)
		return
	}

	secondSVState, err := doc.ToJSON()
	if err != nil {
		fmt.Println("Error getting second state vector JSON:", err)
		return
	}
	fmt.Printf("Second State Vector JSON: %+v\n", secondSVState)

	newDoc := autosyncdoc.NewAutoSyncDoc()
	defer newDoc.Destroy()

	err = newDoc.ApplyStateVector(firstStateVector)
	if err != nil {
		fmt.Println("Error applying first state vector to new doc:", err)
		return
	}

	newDocSVState, err := newDoc.ToJSON()
	if err != nil {
		fmt.Println("Error getting new doc state vector JSON:", err)
		return
	}
	fmt.Printf("New Doc State Vector JSON: %+v\n", newDocSVState)

	err = newDoc.ApplyStateVector(secondStateVector)
	if err != nil {
		fmt.Println("Error applying second state vector to new doc:", err)
		return
	}

	newDocSecondSVState, err := newDoc.ToJSON()
	if err != nil {
		fmt.Println("Error getting new doc second state vector JSON:", err)
		return
	}
	fmt.Printf("New Doc Second State Vector JSON: %+v\n", newDocSecondSVState)
}
