package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
)

const timeout = 30 // Client-Server connection timeout

var messageService *service
var messages int64

type message struct {
	id    int64
	event string
	data  string
}

type service struct {
	sync.RWMutex
	clients map[chan message]string
}

func newService() *service {
	return &service{clients: make(map[chan message]string)}
}

func (b *service) listen(topic string) chan message {
	ch := make(chan message)

	b.Lock()
	defer b.Unlock()
	b.clients[ch] = topic

	return ch
}

func (b *service) drop(ch chan message) {
	b.RLock()
	_, active := b.clients[ch]
	b.RUnlock()
	if active { // Drop connection if still active
		b.Lock()

		delete(b.clients, ch)
		close(ch)

		b.Unlock()
	}
}

func (b *service) post(msg message, topic string) {
	b.RLock()
	defer b.RUnlock()

	for ch, room := range b.clients {
		if _, active := b.clients[ch]; room == topic && active {
			ch <- msg
		}
	}
}

func (b *service) timeoutTimer(ch chan message) {
	time.Sleep(time.Second * timeout)

	b.RLock()
	_, active := b.clients[ch]
	b.RUnlock()
	if active { // Timeout connection if still active
		msg := message{event: "timeout", data: fmt.Sprintf("%ds", timeout)}
		ch <- msg
		b.drop(ch)
	}
}

// PostMessage posts a message to the specified topic
// Successful operation returns a response status code HTTP 204
func PostMessage(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)    // Get request parameters
	topic := params["topic"] // Get topic from URL parameter

	r.ParseForm()

	for key := range r.PostForm {
		atomic.StoreInt64(&messages, atomic.LoadInt64(&messages)+1)
		msg := message{atomic.LoadInt64(&messages), "msg", key}
		messageService.post(msg, topic)
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

	ch := messageService.listen(topic)
	go messageService.timeoutTimer(ch)
	defer messageService.drop(ch)

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
	messageService = newService()

	router := mux.NewRouter()
	router.HandleFunc("/infocenter/{topic}", PostMessage).Methods("POST")
	router.HandleFunc("/infocenter/{topic}", GetMessages).Methods("GET")

	log.Fatal(http.ListenAndServe(":8000", router))
}
