package core

type Result string

const (
	ACK     Result = "ack"
	RETRY   Result = "retry"
	REQUEUE Result = "requeue"
	DROP    Result = "drop"
)
