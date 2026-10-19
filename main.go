package main

import (
    "context"
    "crypto/sha256"
    "crypto/subtle"
    "embed"
    "encoding/json"
    "errors"
    "io"
    "io/fs"
    "log"
    "net/http"
    "net/url"
    "os"
    "os/signal"
    "regexp"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/LuisInacioDasilva/relayforge-go/internal/relay"
)

//go:embed web/*
var webAssets embed.FS

var idPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{3,80}$`)
var kindPattern = regexp.MustCompile(`^[A-Za-z0-9:._-]{3,80}$`)

type incoming struct { ID string `json:"id"`; Kind string `json:"kind"`; Payload json.RawMessage `json:"payload"` }

func authorized(header, key string) bool {
    if !strings.HasPrefix(header, "Bearer ") || key == "" { return false }
    actual := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
    expected := sha256.Sum256([]byte(key))
    return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.Header().Set("Cache-Control", "no-store")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(value)
}

func routes(store *relay.Store, apiKey string) http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"status":"ok", "jobs":len(store.List())}) })
    mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, store.List()) })
    mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
        if !authorized(r.Header.Get("Authorization"), apiKey) { writeJSON(w, 401, map[string]string{"error":"unauthorized"}); return }
        r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
        decoder := json.NewDecoder(r.Body); decoder.DisallowUnknownFields()
        var data incoming
        if err := decoder.Decode(&data); err != nil { writeJSON(w, 400, map[string]string{"error":"invalid json or payload too large"}); return }
        if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) { writeJSON(w, 400, map[string]string{"error":"multiple json values"}); return }
        if !idPattern.MatchString(data.ID) || !kindPattern.MatchString(data.Kind) || len(data.Payload) == 0 || !json.Valid(data.Payload) { writeJSON(w, 422, map[string]string{"error":"invalid event"}); return }
        created, err := store.Enqueue(relay.Job{ID:data.ID, Kind:data.Kind, Payload:data.Payload})
        if err != nil { log.Printf("enqueue: %v",err); writeJSON(w, 500, map[string]string{"error":"storage unavailable"}); return }
        status := 202; if !created { status = 200 }
        writeJSON(w, status, map[string]any{"id":data.ID,"duplicate":!created})
    })
    files, _ := fs.Sub(webAssets, "web")
    mux.Handle("GET /", http.FileServer(http.FS(files)))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
        mux.ServeHTTP(w, r)
    })
}

func main() {
    key := os.Getenv("RELAY_API_KEY"); signing := os.Getenv("RELAY_SIGNING_KEY"); destination := os.Getenv("RELAY_DESTINATION")
    if len(key) < 24 || len(signing) < 24 { log.Fatal("RELAY_API_KEY and RELAY_SIGNING_KEY must be at least 24 characters") }
    target, err := url.Parse(destination)
    if err != nil || target.Host == "" || (target.Scheme != "https" && !(target.Scheme == "http" && (target.Hostname() == "127.0.0.1" || target.Hostname() == "localhost"))) { log.Fatal("RELAY_DESTINATION must be HTTPS or local HTTP") }
    path := os.Getenv("DATA_FILE"); if path == "" { path = "./data/jobs.ndjson" }
    store, err := relay.Open(path); if err != nil { log.Fatal(err) }
    client := &http.Client{Timeout:5*time.Second, CheckRedirect:func(*http.Request,[]*http.Request) error { return http.ErrUseLastResponse }}
    dispatcher := &relay.Dispatcher{Store:store, Destination:destination, SigningKey:[]byte(signing), Client:client}
    port := os.Getenv("PORT"); if port == "" { port = "8080" }; if n,err:=strconv.Atoi(port); err!=nil || n<1 || n>65535 { log.Fatal("invalid PORT") }
    server := &http.Server{Addr:"127.0.0.1:"+port, Handler:routes(store,key), ReadHeaderTimeout:5*time.Second}
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM); defer stop()
    go func() { ticker:=time.NewTicker(time.Second); defer ticker.Stop(); for { select { case <-ctx.Done():return; case <-ticker.C:if err:=dispatcher.Process(ctx);err!=nil { log.Printf("dispatcher: %v",err) } } } }()
    go func() { <-ctx.Done(); shutdown, cancel:=context.WithTimeout(context.Background(),5*time.Second); defer cancel(); server.Shutdown(shutdown) }()
    log.Printf("RelayForge running at http://%s",server.Addr)
    if err:=server.ListenAndServe();err!=nil && !errors.Is(err,http.ErrServerClosed) { log.Fatal(err) }
}
