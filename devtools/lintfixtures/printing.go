package escp

import "fmt"

// PrintFromTheLibrary writes to standard output with each of the five forms the rule names.
func PrintFromTheLibrary() {
	fmt.Print("a")
	fmt.Printf("%s", "b")
	fmt.Println("c")
	print("d")
	println("e")
}
