package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/bradfitz/gomemcache/memcache"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type RequestStat struct {
	Url          string
	Status       int
	Error        error
	ResponseTime time.Duration
	StartTime    time.Time
	AccountId    int
}

type RepeatingRequest struct {
	Id     int
	Url    string
	Every  time.Duration
	Ticker *time.Ticker
}

type Request struct {
	Url       string
	AccountId int
}

var (
	optPort           = flag.String("port", "9001", "Port to run the http server on")
	optConnectTimeout = flag.Int("connect-timeout", 5000, "HTTP connect timeout for polling in milliseconds")
	optRequestTimeout = flag.Int("request-timeout", 10000, "HTTP request timeout for polling in milliseconds")
)

var (
	kestrelClient     = memcache.New("localhost:22133")
	RepeatingRequests = make(map[int]*RepeatingRequest)
	rcs               = make(chan *Request)
)

func buildHttpClient(dialTimeout, timeout time.Duration) *http.Client {
	transport := &http.Transport{Dial: func(netw, addr string) (net.Conn, error) {
		deadline := time.Now().Add(timeout)
		c, err := net.DialTimeout(netw, addr, dialTimeout)
		if err != nil {
			return nil, err
		}
		c.SetDeadline(deadline)

		return c, nil
	}}
	client := &http.Client{Transport: transport}

	return client
}

func doRequest(req *Request) *RequestStat {
	stat := new(RequestStat)
	stat.Url = req.Url
	stat.AccountId = req.AccountId

	httpReq, _ := http.NewRequest("GET", req.Url, nil)
	ct := time.Duration(*optConnectTimeout) * time.Millisecond
	rt := time.Duration(*optRequestTimeout) * time.Millisecond

	start := time.Now()
	resp, err := buildHttpClient(ct, rt).Do(httpReq)
	stat.ResponseTime = time.Now().Sub(start)
	stat.StartTime = start

	if err != nil {
		log.Printf("Failed to fetch %s", req.Url)

		stat.Error = err
	} else {
		defer resp.Body.Close()

		stat.Status = resp.StatusCode
	}

	return stat
}

func startClient(rcs chan *Request, scs chan *RequestStat) {
	for {
		select {
		case r := <-rcs:
			scs <- doRequest(r)
		}
	}
}

func startCollect(cs chan *RequestStat) {
	for {
		select {
		case stat := <-cs:
			statMessage, err := json.Marshal(stat)
			if err != nil {
				log.Printf("Failed to parse %s", stat)
			} else {
				item := &memcache.Item{Key: "stats", Value: []byte(statMessage)}
				err := kestrelClient.Set(item)
				if err != nil {
					log.Printf("Failed to queue stat: %s", err)
				}

				log.Println(string(statMessage))
			}
		}
	}
}

func (r *RepeatingRequest) Start(url string, every time.Duration, id int, rcs chan *Request) {
	r.Url = url
	r.Every = every
	r.Id = id
	r.Ticker = time.NewTicker(every)
	for {
		select {
		case <-r.Ticker.C:
			req := new(Request)
			req.Url = r.Url
			req.AccountId = r.Id
			rcs <- req
		}
	}
}

func (r *RepeatingRequest) Stop() {
	r.Ticker.Stop()
}

func NewRepeatingRequest(url string, every time.Duration, id int, rcs chan *Request) *RepeatingRequest {
	r := new(RepeatingRequest)
	go r.Start(url, every, id, rcs)

	return r
}

func startControl(workerCount int) {
	cs := make(chan *RequestStat)

	log.Println("Spawning collector")
	go startCollect(cs)

	for i := 0; i < workerCount; i++ {
		log.Printf("Spawning client %d\n", i)
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

	RepeatingRequests[id] = NewRepeatingRequest(url, time.Duration(every)*time.Second, id, rcs)

	msg := fmt.Sprintf("Added %d", id)
	log.Println(msg)
	fmt.Fprintf(w, msg)
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

	msg := fmt.Sprintf("Stopped %d", id)
	log.Println(msg)
	fmt.Fprintf(w, msg)
}

func main() {
	flag.Parse()

	workers := 10
	startControl(workers)

	http.HandleFunc("/add", addHandler)
	http.HandleFunc("/remove", removeHandler)

	log.Printf("Running http server on port %s\n", *optPort)
	http.ListenAndServe(fmt.Sprintf(":%s", *optPort), nil)
}
