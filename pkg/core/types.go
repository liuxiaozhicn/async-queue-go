package core

import "fmt"

type Result string

const (
	ACK     Result = "ack"
	RETRY   Result = "retry"
	REQUEUE Result = "requeue"
	DROP    Result = "drop"
)

func (r Result) String() string {
	switch r {
	case ACK:
		return "ACK"
	case RETRY:
		return "RETRY"
	case REQUEUE:
		return "REQUEUE"
	case DROP:
		return "DROP"
	default:
		return fmt.Sprintf("UNKNOWN(%s)", string(r))
	}
}
