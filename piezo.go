package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
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

func requiredParams(form url.Values, params map[string]string, fields ...string) error {
	for _, v := range fields {
		if val, ok := form[v]; ok {
			params[v] = val[0]
		} else {
			return errors.New(fmt.Sprintf("%s is required", v))
		}
	}

	return nil
}

func addHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	params := make(map[string]string)

	err := requiredParams(r.Form, params, "url", "every", "id")

	if err != nil {
		http.Error(w, err.Error(), 400)

		return
	}
	id, _ := strconv.Atoi(params["id"])
	url := params["url"]
	every, _ := strconv.Atoi(params["every"])

	if rr, ok := RepeatingRequests[id]; ok {
		rr.Stop()
	}

	RepeatingRequests[id] = NewRepeatingRequest(url, time.Duration(every)*time.Second, rcs)

	fmt.Fprintf(w, "Added %d", id)
}
func removeHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	params := make(map[string]string)

	err := requiredParams(r.Form, params, "id")

	if err != nil {
		http.Error(w, err.Error(), 400)

		return
	}
	id, _ := strconv.Atoi(params["id"])

	if rr, ok := RepeatingRequests[id]; ok {
		rr.Stop()
	}

	fmt.Fprintf(w, "Stopped %d", id)
}

func main() {
	RepeatingRequests = make(map[int]*RepeatingRequest)
	workers := 10
	startControl(workers)

	http.HandleFunc("/add", addHandler)
	http.HandleFunc("/remove", removeHandler)
	http.ListenAndServe(":9001", nil)
}
