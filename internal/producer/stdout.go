package producer

import (
	"context"
	"fmt"
)

type StdOutPublisher struct{}

func NewStdoutPublisher() *StdOutPublisher {
	return &StdOutPublisher{}
}

func (p *StdOutPublisher) PublishTo(_ context.Context, key string, message []byte, extra map[string]string) ([]byte, error) {
	_, err := fmt.Printf("%s %s [%v]\n\n", key, message, extra)
	return nil, err
}

func (p *StdOutPublisher) Close() error {
	return nil
}
