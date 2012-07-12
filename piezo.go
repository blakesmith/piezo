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

func doStatCollect(cs chan *RequestStat, n int) {
	stats := make([]*RequestStat, n)

	for i := 0; i < len(stats); i++ {
		fmt.Println(<-cs)
	}
}

func main() {
	c := 10
	cs := make(chan *RequestStat)

	for i := 0; i < c; i++ {
		go doRequest("http://blakesmith.me", cs)
	}

	doStatCollect(cs, c)
}
