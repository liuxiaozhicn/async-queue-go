package asyncqueue

// Job is a marker interface for enqueue payload structs.
//
// Any JSON-marshalable struct can be used as a job payload.
type Job interface {
}
