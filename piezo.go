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

type RepeatingRequest struct {
	Url   string
	Every int
}

func doRequest(url string) *RequestStat {
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

	return stat
}

func startClient(rcs chan string, scs chan *RequestStat) {
	for {
		select {
		case url := <-rcs:
			scs <- doRequest(url)
		}
	}
}

func startCollect(cs chan *RequestStat) {
	for {
		select {
		case stat := <-cs:
			fmt.Println(stat)
		}
	}
}

func startControl(workerCount int) {
	rcs := make(chan string)
	cs := make(chan *RequestStat)

	fmt.Println("Spawning collector")
	go startCollect(cs)

	for i := 0; i < workerCount; i++ {
		fmt.Printf("Spawning client %d\n", i)
		go startClient(rcs, cs)
	}

	for {
		for i := 0; i < 1000; i++ {
			rcs <- "http://blakesmith.me"
		}

		fmt.Println("Sleeping")
		time.Sleep(1 * time.Second)
	}
}

func main() {
	workers := 10
	startControl(workers)
}
