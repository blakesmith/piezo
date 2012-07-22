# Piezo
From the greek word _piezein_, meaning to squeeze or press

## What is it?

In its simplest form, Piezo is a simple tool to schedule repeating http requests. It provides a simple http api to schedule recurring requests.

Some things I use it for:

- HTTP pinger and status checker
- Load or stress tester

## Building

[Install Go](http://golang.org/doc/install) then:

```
go get
go build
```

If you want to build for a different architecture or OS, see the instructions for [installing a cross-compiler toolchain](http://code.google.com/p/go-wiki/wiki/WindowsCrossCompiling).

To build on Linux, run:

```
CGO_ENABLED=0 GOOS=linux GOARCH=386 go build
```
## Usage

Piezo includes the following options:

```
  -connect-timeout=5000: HTTP connect timeout for polling in milliseconds
  -enable-kestrel=false: Register kestrel as a request receiver
  -kestrel-host="localhost:22133": Kestrel host:port address
  -port="9001": Port to run the http server on
  -request-timeout=10000: HTTP request timeout for polling in milliseconds
  -worker-count=10: Number of request workers
```

To start piezo with 20 concurrent request workers that dump request stats to Kestrel, run:

```
./piezo -worker-count 20 -enable-kestrel -kestrel-host localhost:22133
```

By default, piezo's http API listens on port 9001 for commands.

```
/add - Add a repeating request

Query params:
      - url: URL escaped url of the url you want to request
      - interval: How often the request should be made, in milliseconds
      - id: Unique id of the repeating request. Needed to remove.

Example:
	Make a request to google.com every 30 seconds:
	
	http://localhost:9001/add?interval=30000&url=http://google.com&id=google
```
	
```      
/remove - Remove a repeating request

Query Params:
      - id: Unique id of the repeating request to remove.

Example:
	Remove a repeating request by id:

	http://localhost:9001/remove?id=google
```

## Stats

Piezo outputs tracks request time and some other request metadata. If you're sending this data to Kestrel, you'll receive a JSON message that looks like this:

```
{
	"Url":"http://localhost:9000/",
	"Status":200,
	"Error":null,
	"ResponseTime":250,
	"StartTime":"2012-07-22T14:34:00.823126-04:00",
	"RepeatingRequestId":"myrequest"
}
```

## About

Piezo is written by Blake Smith.

