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
	Url    string
	Every  time.Duration
	Ticker *time.Ticker
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

func (r *RepeatingRequest) Start(url string, every time.Duration, rcs chan string) {
	r.Url = url
	r.Every = every
	r.Ticker = time.NewTicker(every)
	for {
		select {
		case <-r.Ticker.C:
			rcs <- r.Url
		}
	}
}

func NewRepeatingRequest(url string, every time.Duration, rcs chan string) *RepeatingRequest {
	r := new(RepeatingRequest)
	go r.Start(url, every, rcs)

	return r
}

func startControl(workerCount int, control chan int) {
	rcs := make(chan string)
	cs := make(chan *RequestStat)

	fmt.Println("Spawning collector")
	go startCollect(cs)

	for i := 0; i < workerCount; i++ {
		fmt.Printf("Spawning client %d\n", i)
		go startClient(rcs, cs)
	}

	s := 2
	reqs := make([]*RepeatingRequest, s)
	reqs[0] = NewRepeatingRequest("http://localhost:9000", 1*time.Second, rcs)
	reqs[1] = NewRepeatingRequest("http://blakesmith.me", 3*time.Second, rcs)

	<-control
}

func main() {
	workers := 10
	control := make(chan int)
	startControl(workers, control)
}
