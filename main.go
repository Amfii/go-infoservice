package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

const timeout = 30 // Client-Server connection timeout

var posts = make(chan *postOp)
var creates = make(chan *createOp)
var timeouts = make(chan *timeoutOp)

type postOp struct {
	topic   string
	message string
	resp    chan bool
}

type createOp struct {
	key  chan message
	val  string
	resp chan bool
}

type timeoutOp struct {
	key  chan message
	resp chan bool
}

type message struct {
	id    int
	event string
	data  string
}

type service struct {
	clients map[chan message]string
}

func newService() *service {
	return &service{clients: make(map[chan message]string)}
}

func timeoutTimer(ch chan message) {
	time.Sleep(time.Second * timeout)

	drop := &timeoutOp{ch, make(chan bool)}
	timeouts <- drop
	<-drop.resp
}

// PostMessage posts a message to the specified topic
// Successful operation returns a response status code HTTP 204
func PostMessage(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)    // Get request parameters
	topic := params["topic"] // Get topic from URL parameter

	r.ParseForm()

	for key := range r.PostForm {
		post := &postOp{topic, key, make(chan bool)}
		posts <- post
		<-post.resp
	}

	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusNoContent) // HTTP 204
}

// GetMessages returns an event stream to the response for the message events listening
// Successful request returns HTTP status code 200 to the response header
func GetMessages(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Add("Access-Control-Allow-Origin", "*")

	params := mux.Vars(r)    // Get request parameters
	topic := params["topic"] // Get topic from URL parameter

	// Add new client
	ch := make(chan message)
	create := &createOp{ch, topic, make(chan bool)}
	creates <- create
	<-create.resp

	// Start client timeout timer
	go timeoutTimer(ch)

	for msg, active := <-ch; active; {
		if msg.id != 0 {
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.id, msg.event, msg.data)
		} else {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.event, msg.data)
		}
		f.Flush()

		msg, active = <-ch
	}

	r.Body.Close()
}

func main() {
	go func() {
		messages := 0
		messageService := newService()
		for {
			select {
			case drop := <-timeouts:
				drop.key <- message{event: "timeout", data: fmt.Sprintf("%ds", timeout)}
				delete(messageService.clients, drop.key)
				close(drop.key)
				drop.resp <- true
			case post := <-posts:
				messages++
				for ch, room := range messageService.clients {
					if room == post.topic {
						ch <- message{messages, "msg", post.message}
					}
				}
				post.resp <- true
			case create := <-creates:
				messageService.clients[create.key] = create.val
				create.resp <- true
			}
		}
	}()

	router := mux.NewRouter()
	router.HandleFunc("/infocenter/{topic}", PostMessage).Methods("POST")
	router.HandleFunc("/infocenter/{topic}", GetMessages).Methods("GET")

	log.Fatal(http.ListenAndServe(":8000", router))
}
