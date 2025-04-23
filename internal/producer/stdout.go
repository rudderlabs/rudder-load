package producer

import (
	"context"
	"fmt"
)

type StdOutPublisher struct {
	slotName string
}

func NewStdoutPublisher(slotName string) *StdOutPublisher {
	return &StdOutPublisher{
		slotName: slotName,
	}
}

func (p *StdOutPublisher) PublishTo(_ context.Context, key string, message []byte, extra map[string]string) ([]byte, error) {
	_, err := fmt.Printf("[SlotName: %s] %s %s [%v]\n\n", p.slotName, key, message, extra)
	return nil, err
}

func (p *StdOutPublisher) Close() error {
	return nil
}
