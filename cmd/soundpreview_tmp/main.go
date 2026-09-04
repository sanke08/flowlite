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

	n := len(sound.Samples(sound.Cancel))
	for i := 0; i < 3; i++ {
		p.Play(sound.Cancel)
		time.Sleep(time.Duration(float64(n)/sound.Rate*float64(time.Second)) + 400*time.Millisecond)
	}
}
