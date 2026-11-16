package escp

import (
	stdfmt "fmt"
	stdtime "time"
)

// PrintThroughAnAlias reaches fmt.Println and time.After through renamed imports. Only the type analysis
// of the linter (analyze-types) sees through the alias: matched as written, `stdfmt.Println` would
// escape the pattern.
func PrintThroughAnAlias() {
	stdfmt.Println("a")
	<-stdtime.After(stdtime.Second)
}
