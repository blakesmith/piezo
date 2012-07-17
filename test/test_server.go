package main

import (
	"fmt"
	"net/http"
)

func rootHandle(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello there!")
}

func main() {
	http.HandleFunc("/", rootHandle)
	http.ListenAndServe(":9000", nil)
}
