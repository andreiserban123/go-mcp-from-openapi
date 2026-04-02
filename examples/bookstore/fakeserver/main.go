// fakeserver is a minimal in-memory bookstore HTTP API that implements every
// endpoint in bookstore.yaml.  It is protected by HTTP Basic Auth so you can
// test the MCP demo's WithBasicAuth option end-to-end.
//
// Usage:
//
//	go run ./examples/bookstore/fakeserver
//	go run ./examples/bookstore/fakeserver --addr :9090 --user admin --pass secret
//
// Then point the MCP demo at it:
//
//	go run ./examples/bookstore --base-url http://localhost:8080/v1 \
//	    --http :8888
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// ---- domain types ----------------------------------------------------------

type Book struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Author string  `json:"author"`
	ISBN   string  `json:"isbn,omitempty"`
	Genre  string  `json:"genre,omitempty"`
	Price  float64 `json:"price,omitempty"`
}

type Review struct {
	ID       int    `json:"id"`
	BookID   string `json:"bookId"`
	Rating   int    `json:"rating"`
	Text     string `json:"text"`
	Reviewer string `json:"reviewer,omitempty"`
}

// ---- in-memory store -------------------------------------------------------

type store struct {
	mu      sync.RWMutex
	books   map[string]*Book
	reviews map[string][]*Review
	nextID  int
	nextRev int
}

func newStore() *store {
	s := &store{
		books:   make(map[string]*Book),
		reviews: make(map[string][]*Review),
		nextID:  4,
		nextRev: 1,
	}
	// seed data
	for _, b := range []*Book{
		{ID: "1", Title: "The Go Programming Language", Author: "Alan Donovan", ISBN: "978-0134190440", Genre: "tech", Price: 39.99},
		{ID: "2", Title: "Dune", Author: "Frank Herbert", ISBN: "978-0441013593", Genre: "sci-fi", Price: 14.99},
		{ID: "3", Title: "1984", Author: "George Orwell", ISBN: "978-0451524935", Genre: "fiction", Price: 9.99},
	} {
		s.books[b.ID] = b
	}
	return s
}

// ---- HTTP handlers ---------------------------------------------------------

type server struct {
	store    *store
	username string
	password string
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != s.username || p != s.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="bookstore"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *server) listBooks(w http.ResponseWriter, r *http.Request) {
	genre := r.URL.Query().Get("genre")
	author := r.URL.Query().Get("author")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	s.store.mu.RLock()
	defer s.store.mu.RUnlock()

	var result []*Book
	for _, b := range s.store.books {
		if genre != "" && !strings.EqualFold(b.Genre, genre) {
			continue
		}
		if author != "" && !strings.Contains(strings.ToLower(b.Author), strings.ToLower(author)) {
			continue
		}
		result = append(result, b)
	}

	// simple pagination
	start := (page - 1) * limit
	if start >= len(result) {
		result = nil
	} else {
		end := start + limit
		if end > len(result) {
			end = len(result)
		}
		result = result[start:end]
	}
	if result == nil {
		result = []*Book{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) createBook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title  string  `json:"title"`
		Author string  `json:"author"`
		ISBN   string  `json:"isbn"`
		Genre  string  `json:"genre"`
		Price  float64 `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" || body.Author == "" || body.ISBN == "" {
		http.Error(w, "bad request: title, author and isbn are required", http.StatusBadRequest)
		return
	}

	s.store.mu.Lock()
	id := strconv.Itoa(s.store.nextID)
	s.store.nextID++
	book := &Book{ID: id, Title: body.Title, Author: body.Author, ISBN: body.ISBN, Genre: body.Genre, Price: body.Price}
	s.store.books[id] = book
	s.store.mu.Unlock()

	writeJSON(w, http.StatusCreated, book)
}

func (s *server) getBook(w http.ResponseWriter, r *http.Request, id string) {
	s.store.mu.RLock()
	book, ok := s.store.books[id]
	s.store.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, book)
}

func (s *server) updateBook(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Title  string  `json:"title"`
		Author string  `json:"author"`
		Genre  string  `json:"genre"`
		Price  float64 `json:"price"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.store.mu.Lock()
	book, ok := s.store.books[id]
	if !ok {
		s.store.mu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if body.Title != "" {
		book.Title = body.Title
	}
	if body.Author != "" {
		book.Author = body.Author
	}
	if body.Genre != "" {
		book.Genre = body.Genre
	}
	if body.Price != 0 {
		book.Price = body.Price
	}
	s.store.mu.Unlock()

	writeJSON(w, http.StatusOK, book)
}

func (s *server) deleteBook(w http.ResponseWriter, r *http.Request, id string) {
	s.store.mu.Lock()
	_, ok := s.store.books[id]
	if ok {
		delete(s.store.books, id)
		delete(s.store.reviews, id)
	}
	s.store.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listReviews(w http.ResponseWriter, r *http.Request, bookID string) {
	s.store.mu.RLock()
	_, ok := s.store.books[bookID]
	reviews := append([]*Review(nil), s.store.reviews[bookID]...)
	s.store.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if reviews == nil {
		reviews = []*Review{}
	}
	writeJSON(w, http.StatusOK, reviews)
}

func (s *server) createReview(w http.ResponseWriter, r *http.Request, bookID string) {
	s.store.mu.RLock()
	_, ok := s.store.books[bookID]
	s.store.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var body struct {
		Rating   int    `json:"rating"`
		Text     string `json:"text"`
		Reviewer string `json:"reviewer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Rating < 1 || body.Rating > 5 || body.Text == "" {
		http.Error(w, "bad request: rating (1-5) and text are required", http.StatusBadRequest)
		return
	}

	s.store.mu.Lock()
	rev := &Review{ID: s.store.nextRev, BookID: bookID, Rating: body.Rating, Text: body.Text, Reviewer: body.Reviewer}
	s.store.nextRev++
	s.store.reviews[bookID] = append(s.store.reviews[bookID], rev)
	s.store.mu.Unlock()

	writeJSON(w, http.StatusCreated, rev)
}

func (s *server) searchBooks(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}

	s.store.mu.RLock()
	defer s.store.mu.RUnlock()

	var result []*Book
	for _, b := range s.store.books {
		if strings.Contains(strings.ToLower(b.Title), q) ||
			strings.Contains(strings.ToLower(b.Author), q) ||
			strings.Contains(strings.ToLower(b.Genre), q) {
			result = append(result, b)
		}
	}
	if result == nil {
		result = []*Book{}
	}
	writeJSON(w, http.StatusOK, result)
}

// ---- routing ---------------------------------------------------------------

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// /v1/books
	mux.HandleFunc("/v1/books", s.auth(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.listBooks(w, r)
		case http.MethodPost:
			s.createBook(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// /v1/books/{bookId}  and  /v1/books/{bookId}/reviews
	mux.HandleFunc("/v1/books/", s.auth(func(w http.ResponseWriter, r *http.Request) {
		// strip /v1/books/
		tail := strings.TrimPrefix(r.URL.Path, "/v1/books/")
		parts := strings.SplitN(tail, "/", 2)
		bookID := parts[0]
		if bookID == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if len(parts) == 2 && parts[1] == "reviews" {
			switch r.Method {
			case http.MethodGet:
				s.listReviews(w, r, bookID)
			case http.MethodPost:
				s.createReview(w, r, bookID)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		switch r.Method {
		case http.MethodGet:
			s.getBook(w, r, bookID)
		case http.MethodPut:
			s.updateBook(w, r, bookID)
		case http.MethodDelete:
			s.deleteBook(w, r, bookID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// /v1/search
	mux.HandleFunc("/v1/search", s.auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.searchBooks(w, r)
	}))

	return mux
}

// ---- main ------------------------------------------------------------------

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	user := flag.String("user", "admin", "Basic auth username")
	pass := flag.String("pass", "secret", "Basic auth password")
	flag.Parse()

	srv := &server{store: newStore(), username: *user, password: *pass}

	log.Printf("bookstore fake server on %s  (user=%s pass=%s)", *addr, *user, *pass)
	log.Printf("point the MCP demo at: --base-url http://localhost%s/v1 --basic-auth-user %s --basic-auth-pass %s", *addr, *user, *pass)

	if err := http.ListenAndServe(*addr, srv.routes()); err != nil {
		fmt.Println(err)
	}
}
