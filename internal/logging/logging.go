package logging

import (
	"fmt"
	"log"
)

type Logger struct{}

func New() *Logger { return &Logger{} }

func (l *Logger) Infof(f string, a ...any) {
	log.Printf("[INFO] "+f, a...)
}

func (l *Logger) Errorf(f string, a ...any) {
	log.Printf("[ERROR] "+f, a...)
}

func (l *Logger) Stage(name string, progress int, msg string) {
	log.Printf("[STAGE] %s %d%% %s", name, progress, msg)
}

func (l *Logger) Println(a ...any) { fmt.Println(a...) }
