package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

var messageService *service
var messages []message

type message struct {
	id    int
	event string
	data  string
}

type service struct {
	clients map[chan message]string
}

func newService() *service {
	return &service{make(map[chan message]string)}
}

func (b *service) listen(topic string) chan message {
	ch := make(chan message)
	b.clients[ch] = topic
	return ch
}

func (b *service) drop(ch chan message) {
	delete(b.clients, ch)
}

func (b *service) post(msg message, topic string) {
	for ch, room := range b.clients {
		if room == topic {
			ch <- msg
		}
	}
}

// PostMessage posts a message to the specified topic
// Successful operation returns a response status code HTTP 204
func PostMessage(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)    // Get request parameters
	topic := params["topic"] // Get topic from URL parameter

	r.ParseForm()

	for key := range r.PostForm {
		msg := message{len(messages) + 1, "msg", key}
		messages = append(messages, msg)
		messageService.post(msg, topic)
	}

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

	params := mux.Vars(r)    // Get request parameters
	topic := params["topic"] // Get topic from URL parameter

	ch := messageService.listen(topic)
	defer messageService.drop(ch)

	for {
		msg := <-ch
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.id, msg.event, msg.data)
		fmt.Printf("id: %d\nevent: %s\ndata: %s\n\n", msg.id, msg.event, msg.data)
		f.Flush()
	}
}

func main() {
	messageService = newService()

	router := mux.NewRouter()
	router.HandleFunc("/infocenter/{topic}", PostMessage).Methods("POST")
	router.HandleFunc("/infocenter/{topic}", GetMessages).Methods("GET")

	log.Fatal(http.ListenAndServe(":8000", router))
}
