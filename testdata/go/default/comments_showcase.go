package sample

import "fmt"

// top-level line comment should be gray
func commentsShowcase() {
	/* block comment before code should be gray */
	x := 1 + /* inline block comment in middle of expression */ 2
	y := x /* trailing block comment at end of code */
	z := y // trailing line comment at end of code

	/*
		multiline block comment should be gray on each line
		and preserve span boundaries correctly
	*/

	str1 := "literal with /* not a comment */ inside"
	str2 := "literal with // not a comment either"
	str3 := `raw string with /* and // still not comments`

	fmt.Println(x, y, z, str1, str2, str3) // ending line comment
}
