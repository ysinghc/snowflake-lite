package snowflake_test

import (
	"fmt"

	"github.com/ysinghc/snowflake-lite/snowflake"
)

func ExampleGenerator_Next() {
	g := snowflake.MustNew(7)
	a, b := g.Next(), g.Next()
	fmt.Println(b > a)
	// Output: true
}

func ExampleDecode() {
	g := snowflake.MustNew(42)
	p := snowflake.Decode(g.Next())
	fmt.Println(p.MachineID)
	// Output: 42
}
