package relay

import (
    "bytes"
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "time"
)

type Dispatcher struct { Store *Store; Destination string; SigningKey []byte; Client *http.Client }

func Signature(body []byte, key []byte) string {
    mac := hmac.New(sha256.New, key); mac.Write(body)
    return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (d *Dispatcher) Deliver(ctx context.Context, job Job) error {
    body, err := json.Marshal(struct { ID string `json:"id"`; Kind string `json:"kind"`; Payload json.RawMessage `json:"payload"` }{job.ID, job.Kind, job.Payload})
    if err != nil { return err }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Destination, bytes.NewReader(body))
    if err != nil { return err }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Relay-Signature", Signature(body, d.SigningKey))
    req.Header.Set("Idempotency-Key", job.ID)
    client := d.Client
    if client == nil { client = &http.Client{Timeout: 5 * time.Second} }
    response, err := client.Do(req)
    if err != nil { return err }
    defer response.Body.Close()
    io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
    if response.StatusCode < 200 || response.StatusCode >= 300 { return fmt.Errorf("receiver returned HTTP %d", response.StatusCode) }
    return nil
}

func (d *Dispatcher) Process(ctx context.Context) error {
    var failures []error
    for _, job := range d.Store.Due(time.Now()) {
        err := d.Deliver(ctx, job)
        job.Attempts++
        if err == nil { job.Status = "delivered"; job.LastError = "" } else {
            job.LastError = err.Error()
            if len(job.LastError) > 200 { job.LastError = job.LastError[:200] }
            if job.Attempts >= 4 { job.Status = "dead" } else { job.NextAt = time.Now().UTC().Add(time.Duration(1<<uint(job.Attempts-1)) * 5 * time.Second) }
        }
        if writeErr := d.Store.Update(job); writeErr != nil { failures = append(failures, writeErr) }
    }
    return errors.Join(failures...)
}
