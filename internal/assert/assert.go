package assert

func Asssert(cond bool, msg string) {

	if !cond {

		panic(msg)

	}

}
