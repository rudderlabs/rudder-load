package producer

import (
	"context"
	"fmt"
)

type StdOutPublisher struct {
	slotName string
	discard  bool
}

func NewStdoutPublisher(slotName string, environ []string) (*StdOutPublisher, error) {
	conf, err := readConfiguration("STDOUT_", environ)
	if err != nil {
		return nil, fmt.Errorf("cannot read stdout configuration: %v", err)
	}

	discard, err := getOptionalBoolSetting(conf, "discard", false)
	if err != nil {
		return nil, err
	}

	return &StdOutPublisher{
		slotName: slotName,
		discard:  discard,
	}, nil
}

func (p *StdOutPublisher) PublishTo(_ context.Context, key string, message []byte, extra map[string]string) ([]byte, error) {
	if p.discard {
		return nil, nil
	}
	_, err := fmt.Printf("[SlotName: %s] %s %s [%v]\n\n", p.slotName, key, message, extra)
	return nil, err
}

func (p *StdOutPublisher) Close() error {
	return nil
}
