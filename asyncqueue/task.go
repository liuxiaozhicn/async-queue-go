package asyncqueue

// Task is a marker interface for enqueue payload structs.
//
// Any JSON-marshalable struct can be used as a message payload.
type Task interface{}
