package main

import (
	"log"
	"time"
)

func main() {
	sessionTimeout := 2 * time.Second
	disco, err := Create([]string{"127.0.0.1:2181", "127.0.0.1:2182", "127.0.0.1:2183"}, sessionTimeout)
	if err != nil {
		panic(err)
	}
	defer disco.Close()

	for {
		select {
		case err := <-disco.ErrorEvents:
			panic(err)
		case e := <-disco.DiscoEvents:
			log.Printf("Got discovery event %v", e)
		}
	}
}
