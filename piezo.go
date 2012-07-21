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
	Id       int
	Url      string
	Interval time.Duration
	Ticker   *time.Ticker
}

type Request struct {
	Url            string
	Method         string
	ConnectTimeout time.Duration
	RequestTimeout time.Duration
	AccountId      int
}

type Options struct {
	Port           *string
	ConnectTimeout *int
	RequestTimeout *int
	WorkerCount    *int
}

type PiezoAgent struct {
	/*	KestrelClient string,
		RepeatingRequets map[int]*RepeatingRequest,
		RequestChannel chan *Request,*/
	Opts Options
}

func (agent *PiezoAgent) Setup() {
	agent.Opts.Port = flag.String("port", "9001", "Port to run the http server on")
	agent.Opts.ConnectTimeout = flag.Int("connect-timeout", 5000, "HTTP connect timeout for polling in milliseconds")
	agent.Opts.RequestTimeout = flag.Int("request-timeout", 10000, "HTTP request timeout for polling in milliseconds")
	agent.Opts.WorkerCount = flag.Int("worker-count", 10, "Number of request workers")
	flag.Parse()
}

func (agent *PiezoAgent) Start() {
	agent.StartControl(*agent.Opts.WorkerCount)
}

func (agent *PiezoAgent) StartControl(workerCount int) {
	cs := make(chan *RequestStat)

	log.Println("Spawning collector")
	go agent.StartCollect(cs)

	for i := 0; i < workerCount; i++ {
		log.Printf("Spawning client %d\n", i)
		go agent.StartClient(rcs, cs)
	}
}

func (agent *PiezoAgent) StartClient(rcs chan *Request, scs chan *RequestStat) {
	for {
		select {
		case r := <-rcs:
			scs <- r.Do()
		}
	}
}

func (agent *PiezoAgent) StartCollect(cs chan *RequestStat) {
	for {
		select {
		case stat := <-cs:
			stat.Queue(false)
			log.Println(stat)
		}
	}
}

var (
	kestrelClient     = memcache.New("localhost:22133")
	RepeatingRequests = make(map[int]*RepeatingRequest)
	rcs               = make(chan *Request)
)

var (
	piezoAgent *PiezoAgent
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

func (req *Request) Do() *RequestStat {
	stat := new(RequestStat)
	stat.Url = req.Url
	stat.AccountId = req.AccountId

	httpReq, _ := http.NewRequest(req.Method, req.Url, nil)

	start := time.Now()
	resp, err := buildHttpClient(req.ConnectTimeout, req.RequestTimeout).Do(httpReq)
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

func (stat *RequestStat) Queue(enabled bool) error {
	if enabled {
		statMessage, err := json.Marshal(stat)
		if err != nil {
			log.Printf("Failed to parse %s", stat)

			return err
		} else {
			item := &memcache.Item{Key: "stats", Value: []byte(statMessage)}
			err := kestrelClient.Set(item)
			if err != nil {
				log.Printf("Failed to queue stat: %s", err)

				return err
			}
		}
	}

	return nil
}

func (r *RepeatingRequest) Start(url string, id int, rcs chan *Request, interval, requestTimeout, connectTimeout time.Duration) {
	r.Url = url
	r.Interval = interval
	r.Id = id
	r.Ticker = time.NewTicker(interval)
	for {
		select {
		case <-r.Ticker.C:
			req := new(Request)
			req.Url = r.Url
			req.AccountId = r.Id
			req.ConnectTimeout = connectTimeout
			req.RequestTimeout = requestTimeout
			req.Method = "GET"
			rcs <- req
		}
	}
}

func (r *RepeatingRequest) Stop() {
	r.Ticker.Stop()
}

func NewRepeatingRequest(url string, id int, rcs chan *Request, interval, requestTimeout, connectTimeout time.Duration) *RepeatingRequest {
	r := new(RepeatingRequest)
	go r.Start(url, id, rcs, interval, requestTimeout, connectTimeout)

	return r
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

	err := requiredParams(r.Form, params, "url", "interval", "id")

	if err != nil {
		http.Error(w, err.Error(), 400)

		return
	}
	id, _ := strconv.Atoi(params["id"])
	url := params["url"]
	interval, _ := strconv.Atoi(params["interval"])

	if rr, ok := RepeatingRequests[id]; ok {
		rr.Stop()
	}

	RepeatingRequests[id] = NewRepeatingRequest(url, id, rcs, time.Duration(interval)*time.Millisecond,
		time.Duration(*piezoAgent.Opts.RequestTimeout)*time.Millisecond,
		time.Duration(*piezoAgent.Opts.ConnectTimeout)*time.Millisecond)

	msg := fmt.Sprintf("Added %d\n", id)
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

	msg := fmt.Sprintf("Stopped %d\n", id)
	log.Println(msg)
	fmt.Fprintf(w, msg)
}

func main() {
	piezoAgent = new(PiezoAgent)
	piezoAgent.Setup()
	piezoAgent.Start()

	http.HandleFunc("/add", addHandler)
	http.HandleFunc("/remove", removeHandler)

	log.Printf("Running http server on port %s\n", *piezoAgent.Opts.Port)
	http.ListenAndServe(fmt.Sprintf(":%s", *piezoAgent.Opts.Port), nil)
}
