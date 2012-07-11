package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

type RequestStats struct {
	Url          string
	Status       int
	ResponseTime time.Duration
}

func doRequest(url string, cs chan string) {
	resp, err := http.Get(url)

	if err != nil {
		log.Printf("Failed to fetch %s", url)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Printf("Failed to read the response body!")
	}

	cs <- string(body)
}

func main() {
	cs := make(chan string)

	go doRequest("http://blakesmith.me", cs)

	fmt.Println(<-cs)
}
