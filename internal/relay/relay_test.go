package relay

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"
)

func TestStoreReplayAndIdempotency(t *testing.T) {
    path := filepath.Join(t.TempDir(), "jobs.ndjson")
    store, err := Open(path); if err != nil { t.Fatal(err) }
    job := Job{ID:"evt-001",Kind:"order.created",Payload:json.RawMessage(`{"order":42}`)}
    created,err:=store.Enqueue(job);if err!=nil||!created { t.Fatalf("first enqueue: %v %v",created,err) }
    created,err=store.Enqueue(job);if err!=nil||created { t.Fatalf("duplicate: %v %v",created,err) }
    restored,err:=Open(path);if err!=nil { t.Fatal(err) }
    if got:=len(restored.List());got!=1 { t.Fatalf("got %d jobs",got) }
}

func TestDeliverySignsBodyAndTransitions(t *testing.T) {
    receiver:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
        if r.Header.Get("Idempotency-Key")!="evt-001" { t.Error("missing idempotency key") }
        body,err:=io.ReadAll(r.Body);if err!=nil { t.Error(err) }
        if r.Header.Get("X-Relay-Signature")!=Signature(body,[]byte("signing-secret")) { t.Error("invalid signature") }
        w.WriteHeader(http.StatusNoContent)
    }));defer receiver.Close()
    store,err:=Open(filepath.Join(t.TempDir(),"jobs.ndjson"));if err!=nil { t.Fatal(err) }
    store.Enqueue(Job{ID:"evt-001",Kind:"order.created",Payload:json.RawMessage(`{"order":42}`)})
    dispatcher:=Dispatcher{Store:store,Destination:receiver.URL,SigningKey:[]byte("signing-secret"),Client:receiver.Client()}
    if err:=dispatcher.Process(context.Background());err!=nil { t.Fatal(err) }
    result:=store.List()[0]
    if result.Status!="delivered"||result.Attempts!=1 { t.Fatalf("unexpected result: %+v",result) }
}

func TestFailedReceiverRemainsPending(t *testing.T) {
    receiver:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){http.Error(w,"try later",503)}));defer receiver.Close()
    store,_:=Open(filepath.Join(t.TempDir(),"jobs.ndjson"))
    store.Enqueue(Job{ID:"evt-001",Kind:"order.created",Payload:json.RawMessage(`{}`)})
    dispatcher:=Dispatcher{Store:store,Destination:receiver.URL,SigningKey:[]byte("key"),Client:receiver.Client()}
    if err:=dispatcher.Process(context.Background());err!=nil { t.Fatal(err) }
    job:=store.List()[0]
    if job.Status!="pending"||job.Attempts!=1||job.LastError=="" { t.Fatalf("unexpected retry state: %+v",job) }
}
