package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

type RequestStat struct {
	Url          string
	Status       int
	ResponseTime time.Duration
	StartTime    time.Time
}

type RepeatingRequest struct {
	Url    string
	Every  time.Duration
	Ticker *time.Ticker
}

var RepeatingRequests map[int]*RepeatingRequest
var rcs chan string

func doRequest(url string) *RequestStat {
	stat := new(RequestStat)
	stat.Url = url

	start := time.Now()
	resp, err := http.Get(url)
	stat.ResponseTime = time.Now().Sub(start)
	stat.StartTime = start

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

func (r *RepeatingRequest) Stop() {
	r.Ticker.Stop()
}

func NewRepeatingRequest(url string, every time.Duration, rcs chan string) *RepeatingRequest {
	r := new(RepeatingRequest)
	go r.Start(url, every, rcs)

	return r
}

func startControl(workerCount int) {
	rcs = make(chan string)
	cs := make(chan *RequestStat)

	fmt.Println("Spawning collector")
	go startCollect(cs)

	for i := 0; i < workerCount; i++ {
		fmt.Printf("Spawning client %d\n", i)
		go startClient(rcs, cs)
	}
}

func addHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	var url string
	var every int
	var id int

	if val, ok := r.Form["url"]; ok {
		url = val[0]
	} else {
		http.Error(w, "url is required", 400)

		return
	}

	if val, ok := r.Form["every"]; ok {
		every, _ = strconv.Atoi(val[0])
	} else {
		http.Error(w, "every is required", 400)

		return
	}

	if val, ok := r.Form["id"]; ok {
		id, _ = strconv.Atoi(val[0])
	} else {
		http.Error(w, "id is required", 400)
	}

	if rr, ok := RepeatingRequests[id]; ok {
		rr.Stop()
	}

	RepeatingRequests[id] = NewRepeatingRequest(url, time.Duration(every)*time.Second, rcs)

	fmt.Fprintf(w, "Added %d", id)
}

func main() {
	RepeatingRequests = make(map[int]*RepeatingRequest)
	workers := 10
	startControl(workers)

	http.HandleFunc("/add", addHandler)
	http.ListenAndServe(":9001", nil)
}
