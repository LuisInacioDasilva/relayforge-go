package relay

import (
    "bufio"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "sync"
    "time"
)

type Job struct {
    ID string `json:"id"`
    Kind string `json:"kind"`
    Payload json.RawMessage `json:"payload"`
    Status string `json:"status"`
    Attempts int `json:"attempts"`
    CreatedAt time.Time `json:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt"`
    NextAt time.Time `json:"nextAt"`
    LastError string `json:"lastError,omitempty"`
}

type Store struct { mu sync.Mutex; path string; jobs map[string]Job }

func Open(path string) (*Store, error) {
    if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { return nil, err }
    file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0600)
    if err != nil { return nil, err }
    defer file.Close()
    store := &Store{path: path, jobs: make(map[string]Job)}
    scanner := bufio.NewScanner(file)
    scanner.Buffer(make([]byte, 4096), 1024*1024)
    line := 0
    for scanner.Scan() {
        line++
        var job Job
        if err := json.Unmarshal(scanner.Bytes(), &job); err != nil { return nil, fmt.Errorf("log line %d: %w", line, err) }
        if job.ID == "" { return nil, fmt.Errorf("log line %d: missing id", line) }
        store.jobs[job.ID] = job
    }
    if err := scanner.Err(); err != nil { return nil, err }
    return store, nil
}

func (s *Store) write(job Job) error {
    file, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
    if err != nil { return err }
    defer file.Close()
    line, err := json.Marshal(job)
    if err != nil { return err }
    if _, err = file.Write(append(line, '\n')); err != nil { return err }
    return file.Sync()
}

func (s *Store) Enqueue(job Job) (bool, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    if _, exists := s.jobs[job.ID]; exists { return false, nil }
    now := time.Now().UTC(); job.CreatedAt = now; job.UpdatedAt = now; job.NextAt = now; job.Status = "pending"
    if err := s.write(job); err != nil { return false, err }
    s.jobs[job.ID] = job
    return true, nil
}

func (s *Store) Update(job Job) error {
    s.mu.Lock(); defer s.mu.Unlock()
    current, exists := s.jobs[job.ID]
    if !exists { return errors.New("job not found") }
    if current.Status == "delivered" || current.Status == "dead" { return errors.New("terminal job") }
    job.CreatedAt = current.CreatedAt; job.UpdatedAt = time.Now().UTC()
    if err := s.write(job); err != nil { return err }
    s.jobs[job.ID] = job
    return nil
}

func (s *Store) List() []Job {
    s.mu.Lock(); defer s.mu.Unlock()
    jobs := make([]Job, 0, len(s.jobs))
    for _, job := range s.jobs { jobs = append(jobs, job) }
    sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
    return jobs
}

func (s *Store) Due(now time.Time) []Job {
    jobs := s.List(); due := make([]Job, 0)
    for _, job := range jobs { if job.Status == "pending" && !job.NextAt.After(now) { due = append(due, job) } }
    return due
}
