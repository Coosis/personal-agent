package sseutil

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type Event struct {
	Event string
	Data  string
}

func Consume(r io.Reader, fn func(Event) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	var dataLines []string

	dispatch := func() error {
		if eventType == "" && len(dataLines) == 0 {
			return nil
		}

		event := Event{
			Event: eventType,
			Data:  strings.Join(dataLines, "\n"),
		}

		eventType = ""
		dataLines = dataLines[:0]
		return fn(event)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, found := strings.Cut(line, ":")
		if !found {
			field = line
			value = ""
		} else if strings.HasPrefix(value, " ") {
			value = value[1:]
		}

		switch field {
		case "event":
			eventType = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan sse stream: %w", err)
	}

	return dispatch()
}
