package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"

    "github.com/LuisInacioDasilva/relayforge-go/internal/relay"
)

func TestAPIAuthenticationAndDeduplication(t *testing.T) {
    store,err:=relay.Open(filepath.Join(t.TempDir(),"jobs.ndjson"));if err!=nil { t.Fatal(err) }
    server:=httptest.NewServer(routes(store,"long-development-api-key-123456"));defer server.Close()
    body:=[]byte(`{"id":"evt-001","kind":"order.created","payload":{"order":42}}`)
    send:=func(token string)*http.Response { req,_:=http.NewRequest(http.MethodPost,server.URL+"/api/jobs",bytes.NewReader(body));req.Header.Set("Authorization",token);response,err:=http.DefaultClient.Do(req);if err!=nil { t.Fatal(err) };return response }
    response:=send("");if response.StatusCode!=401 { t.Fatalf("unauthorized: %d",response.StatusCode) };response.Body.Close()
    response=send("Bearer long-development-api-key-123456");if response.StatusCode!=202 { t.Fatalf("first: %d",response.StatusCode) };response.Body.Close()
    response=send("Bearer long-development-api-key-123456");if response.StatusCode!=200 { t.Fatalf("duplicate: %d",response.StatusCode) };response.Body.Close()
    response,err=http.Get(server.URL+"/api/jobs");if err!=nil { t.Fatal(err) };defer response.Body.Close()
    var jobs []relay.Job;if err:=json.NewDecoder(response.Body).Decode(&jobs);err!=nil { t.Fatal(err) };if len(jobs)!=1 { t.Fatalf("got %d jobs",len(jobs)) }
}
