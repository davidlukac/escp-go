package escp

import "log"

// LogFromTheLibrary uses the log package, which the library never does: one call of each family of the
// rule (Print, Fatal and Panic, with the f and ln forms among them). The Fatal and Panic calls are
// never run.
func LogFromTheLibrary(never bool) {
	log.Print("a")
	if never {
		log.Fatalf("%s", "b")
		log.Panicln("c")
	}
}
