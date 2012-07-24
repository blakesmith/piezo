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
	Url                string
	Status             int
	Error              error
	ResponseTime       time.Duration
	StartTime          time.Time
	RepeatingRequestId string
}

type RepeatingRequest struct {
	Id       string
	Url      string
	Interval time.Duration
	Ticker   *time.Ticker
}

type Request struct {
	Url                string
	Method             string
	RepeatingRequestId string
}

type Options struct {
	Port           *string
	ConnectTimeout *int
	RequestTimeout *int
	WorkerCount    *int
	KestrelHost    *string
	EnableKestrel  *bool
	KestrelQueue   *string
}

type Agent struct {
	RepeatingRequests map[string]*RepeatingRequest
	RequestChannel    chan *Request
	Receivers         []Receiver
	Opts              Options
}

type Receiver interface {
	Queue(stat *RequestStat) error
}

type RequestParams url.Values
type KestrelClient struct {
	Cache     *memcache.Client
	QueueName string
}

type AddHandler struct {
	Agent *Agent
}

type RemoveHandler struct {
	Agent *Agent
}

func (agent *Agent) ParseOpts() {
	agent.Opts.Port = flag.String("port", "9001", "Port to run the http server on")
	agent.Opts.ConnectTimeout = flag.Int("connect-timeout", 5000, "HTTP connect timeout for polling in milliseconds")
	agent.Opts.RequestTimeout = flag.Int("request-timeout", 10000, "HTTP request timeout for polling in milliseconds")
	agent.Opts.WorkerCount = flag.Int("worker-count", 10, "Number of request workers")
	agent.Opts.KestrelHost = flag.String("kestrel-host", "localhost:22133", "Kestrel host:port address")
	agent.Opts.EnableKestrel = flag.Bool("enable-kestrel", false, "Register kestrel as a request receiver")
	agent.Opts.KestrelQueue = flag.String("kestrel-queue", "stats", "Name of the kestrel queue")
	flag.Parse()
}

func (agent *Agent) Setup() {
	agent.Receivers = make([]Receiver, 0)

	if *agent.Opts.EnableKestrel {
		kestrel := new(KestrelClient)
		kestrel.Cache = memcache.New(*agent.Opts.KestrelHost)
		kestrel.QueueName = *agent.Opts.KestrelQueue
		agent.RegisterReceiver(kestrel)
	}

	agent.RepeatingRequests = make(map[string]*RepeatingRequest)
	agent.RequestChannel = make(chan *Request)
}

func (agent *Agent) Start() {
	cs := make(chan *RequestStat)

	log.Println("Spawning collector")
	go agent.StartCollect(cs)

	for i := 0; i < *agent.Opts.WorkerCount; i++ {
		log.Printf("Spawning client %d\n", i)
		go agent.StartClient(agent.RequestChannel, cs)
	}
}

func (agent *Agent) StartClient(rcs chan *Request, scs chan *RequestStat) {
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

func (agent *Agent) StartCollect(cs chan *RequestStat) {
	for {
		select {
		case stat := <-cs:
			for _, rec := range agent.Receivers {
				rec.Queue(stat)
			}
			log.Println(stat)
		}
	}
}

func (agent *Agent) StopRepeatingRequest(id string) {
	if rr, ok := agent.RepeatingRequests[id]; ok {
		rr.Stop()
	}
}
func (agent *Agent) AddRepeatingRequest(id, url string, interval time.Duration) *RepeatingRequest {
	r := new(RepeatingRequest)
	r.Id = id
	r.Url = url
	r.Interval = interval
	r.Ticker = time.NewTicker(interval)

	go r.Start(agent.RequestChannel)

	agent.RepeatingRequests[id] = r

	return r
}

func (agent *Agent) RegisterReceiver(rec Receiver) {
	agent.Receivers = append(agent.Receivers, rec)
}

func (k *KestrelClient) Queue(stat *RequestStat) error {
	statMessage, err := json.Marshal(stat)
	if err != nil {
		log.Printf("Failed to parse %s", stat)

		return err
	} else {
		log.Println(string(statMessage))
		item := &memcache.Item{Key: k.QueueName, Value: []byte(statMessage)}
		err := k.Cache.Set(item)
		if err != nil {
			log.Printf("Failed to queue stat: %s", err)

			return err
		}
	}

	return nil
}

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
	stat.RepeatingRequestId = req.RepeatingRequestId

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

func (r *RepeatingRequest) Start(requestChannel chan *Request) {
	for {
		select {
		case <-r.Ticker.C:
			req := new(Request)
			req.Url = r.Url
			req.RepeatingRequestId = r.Id
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
	id := params["id"]
	url := params["url"]
	interval, _ := strconv.Atoi(params["interval"])

	h.Agent.StopRepeatingRequest(id)
	h.Agent.AddRepeatingRequest(id, url, time.Duration(interval)*time.Millisecond)

	msg := fmt.Sprintf("Added %s\n", id)
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
	id := params["id"]

	h.Agent.StopRepeatingRequest(id)

	msg := fmt.Sprintf("Stopped %s\n", id)
	log.Println(msg)
	fmt.Fprintf(w, msg)
}

func main() {
	agent := new(Agent)
	agent.ParseOpts()

	flag.Args()

	agent.Setup()
	agent.Start()

	add := new(AddHandler)
	remove := new(RemoveHandler)

	add.Agent = agent
	remove.Agent = agent

	http.Handle("/add", add)
	http.Handle("/remove", remove)

	log.Printf("Running http server on port %s\n", *agent.Opts.Port)
	err := http.ListenAndServe(fmt.Sprintf(":%s", *agent.Opts.Port), nil)
	if err != nil {
		log.Printf(err.Error())
	}
}
