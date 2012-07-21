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
	Url       string
	Method    string
	AccountId int
}

type Options struct {
	Port           *string
	ConnectTimeout *int
	RequestTimeout *int
	WorkerCount    *int
}

type PiezoAgent struct {
	KestrelClient     *memcache.Client
	RepeatingRequests map[int]*RepeatingRequest
	RequestChannel    chan *Request
	Opts              Options
}

type RequestParams url.Values
type AddHandler struct {
	Agent *PiezoAgent
}

type RemoveHandler struct {
	Agent *PiezoAgent
}

func (agent *PiezoAgent) ParseOpts() {
	agent.Opts.Port = flag.String("port", "9001", "Port to run the http server on")
	agent.Opts.ConnectTimeout = flag.Int("connect-timeout", 5000, "HTTP connect timeout for polling in milliseconds")
	agent.Opts.RequestTimeout = flag.Int("request-timeout", 10000, "HTTP request timeout for polling in milliseconds")
	agent.Opts.WorkerCount = flag.Int("worker-count", 10, "Number of request workers")
	flag.Parse()
}

func (agent *PiezoAgent) Setup() {
	agent.KestrelClient = memcache.New("localhost:22133")
	agent.RepeatingRequests = make(map[int]*RepeatingRequest)
	agent.RequestChannel = make(chan *Request)
}

func (agent *PiezoAgent) Start() {
	cs := make(chan *RequestStat)

	log.Println("Spawning collector")
	go agent.StartCollect(cs)

	for i := 0; i < *agent.Opts.WorkerCount; i++ {
		log.Printf("Spawning client %d\n", i)
		go agent.StartClient(agent.RequestChannel, cs)
	}
}

func (agent *PiezoAgent) StartClient(rcs chan *Request, scs chan *RequestStat) {
	ct := time.Duration(*agent.Opts.ConnectTimeout) * time.Millisecond
	rt := time.Duration(*agent.Opts.RequestTimeout) * time.Millisecond
	client := buildHttpClient(ct, rt)
	for {
		select {
		case r := <-rcs:
			scs <- r.Do(client)
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

func (agent *PiezoAgent) StopRepeatingRequest(id int) {
	if rr, ok := agent.RepeatingRequests[id]; ok {
		rr.Stop()
	}
}
func (agent *PiezoAgent) AddRepeatingRequest(id int, url string, interval time.Duration) *RepeatingRequest {
	r := new(RepeatingRequest)
	r.Id = id
	r.Url = url
	r.Interval = interval
	r.Ticker = time.NewTicker(interval)

	go r.Start(agent.RequestChannel)

	agent.RepeatingRequests[id] = r

	return r
}

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

func (req *Request) Do(client *http.Client) *RequestStat {
	stat := new(RequestStat)
	stat.Url = req.Url
	stat.AccountId = req.AccountId

	httpReq, _ := http.NewRequest(req.Method, req.Url, nil)

	start := time.Now()
	resp, err := client.Do(httpReq)
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
			err := piezoAgent.KestrelClient.Set(item)
			if err != nil {
				log.Printf("Failed to queue stat: %s", err)

				return err
			}
		}
	}

	return nil
}

func (r *RepeatingRequest) Start(requestChannel chan *Request) {
	for {
		select {
		case <-r.Ticker.C:
			req := new(Request)
			req.Url = r.Url
			req.AccountId = r.Id
			req.Method = "GET"
			requestChannel <- req
		}
	}
}

func (r *RepeatingRequest) Stop() {
	r.Ticker.Stop()
}

func (form RequestParams) RequiredParams(fields ...string) (map[string]string, error) {
	params := make(map[string]string)
	for _, v := range fields {
		if val, ok := form[v]; ok {
			params[v] = val[0]
		} else {
			return nil, errors.New(fmt.Sprintf("%s is required", v))
		}
	}

	return params, nil
}

func (h AddHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	params, err := RequestParams(r.Form).RequiredParams("url", "interval", "id")

	if err != nil {
		http.Error(w, err.Error(), 400)

		return
	}
	id, _ := strconv.Atoi(params["id"])
	url := params["url"]
	interval, _ := strconv.Atoi(params["interval"])

	h.Agent.StopRepeatingRequest(id)
	h.Agent.AddRepeatingRequest(id, url, time.Duration(interval)*time.Millisecond)

	msg := fmt.Sprintf("Added %d\n", id)
	log.Println(msg)
	fmt.Fprintf(w, msg)
}

func (h RemoveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	params, err := RequestParams(r.Form).RequiredParams("id")

	if err != nil {
		http.Error(w, err.Error(), 400)

		return
	}
	id, _ := strconv.Atoi(params["id"])

	h.Agent.StopRepeatingRequest(id)

	msg := fmt.Sprintf("Stopped %d\n", id)
	log.Println(msg)
	fmt.Fprintf(w, msg)
}

func main() {
	agent := new(PiezoAgent)
	piezoAgent = agent
	agent.ParseOpts()
	agent.Setup()
	agent.Start()

	add := new(AddHandler)
	remove := new(RemoveHandler)

	add.Agent = agent
	remove.Agent = agent

	http.Handle("/add", add)
	http.Handle("/remove", remove)

	log.Printf("Running http server on port %s\n", *agent.Opts.Port)
	http.ListenAndServe(fmt.Sprintf(":%s", *agent.Opts.Port), nil)
}
