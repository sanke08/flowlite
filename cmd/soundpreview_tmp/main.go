package main

import (
	"fmt"
	"time"

	"github.com/sanke08/flowlite/internal/sound"
)

func main() {
	p, err := sound.NewPlayer(true)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer p.Close()

	p.StartWorking()
	time.Sleep(3 * time.Second)
	p.StopWorking()
}
