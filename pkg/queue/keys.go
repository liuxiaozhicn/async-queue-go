package queue

import "fmt"

type Keys struct {
	Channel       string
	Waiting       string
	Reserved      string
	Timeout       string
	Delayed       string
	Failed        string
	MessagePrefix string
	SequenceKey   string
	SequenceEpoch string
}

func NewKeys(channel string) Keys {
	return Keys{
		Channel:       channel,
		Waiting:       fmt.Sprintf("{%s}:waiting", channel),
		Reserved:      fmt.Sprintf("{%s}:reserved", channel),
		Timeout:       fmt.Sprintf("{%s}:timeout", channel),
		Delayed:       fmt.Sprintf("{%s}:delayed", channel),
		Failed:        fmt.Sprintf("{%s}:failed", channel),
		MessagePrefix: fmt.Sprintf("{%s}:message:", channel),
		SequenceKey:   fmt.Sprintf("{%s}:msg_seq", channel),
		SequenceEpoch: fmt.Sprintf("{%s}:msg_seq_epoch", channel),
	}
}

func (k Keys) Get(name string) (string, error) {
	switch name {
	case "waiting":
		return k.Waiting, nil
	case "reserved":
		return k.Reserved, nil
	case "timeout":
		return k.Timeout, nil
	case "delayed":
		return k.Delayed, nil
	case "failed":
		return k.Failed, nil
	default:
		return "", fmt.Errorf("queue %s is not supported", name)
	}
}

func (k Keys) Message(id string) string {
	return k.MessagePrefix + id
}
