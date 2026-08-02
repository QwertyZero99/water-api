package main

import (
	"log"
	"net/http"
)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	// static files
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// js seperate ig
	fs = http.FileServer(http.Dir("./js"))
	http.Handle("/js/", http.StripPrefix("/js/", fs))

	http.HandleFunc("/", homeHandler)

	log.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
