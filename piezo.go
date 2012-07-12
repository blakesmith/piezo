package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

type RequestStat struct {
	Url          string
	Status       int
	ResponseTime time.Duration
}

func doRequest(url string, cs chan *RequestStat) {
	stat := new(RequestStat)
	stat.Url = url

	start := time.Now()
	resp, err := http.Get(url)
	stat.ResponseTime = time.Now().Sub(start)

	if err != nil {
		log.Printf("Failed to fetch %s", url)
	}
	defer resp.Body.Close()

	stat.Status = resp.StatusCode

	cs <- stat
}

func main() {
	cs := make(chan *RequestStat)

	go doRequest("http://blakesmith.me", cs)

	fmt.Println(<-cs)
}
