package status

import (
	"encoding/json"
	"os"
	"time"
)

type State struct {
	Stage     string    `json:"stage"`
	Progress  int       `json:"progress"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Store struct{ file string }

func NewStore(file string) *Store { return &Store{file: file} }

func (s *Store) Set(stage string, progress int, msg string) error {
	st := State{Stage: stage, Progress: progress, Message: msg, UpdatedAt: time.Now()}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.file, b, 0o644)
}

func (s *Store) Read() (*State, error) {
	b, err := os.ReadFile(s.file)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Stage: "idle", Progress: 0, Message: "not started", UpdatedAt: time.Now()}, nil
		}
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *State) JSON() string {
	b, _ := json.Marshal(s)
	return string(b)
}
