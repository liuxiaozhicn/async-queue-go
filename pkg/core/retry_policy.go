package core

func RetrySeconds(retrySeconds []int, attempts int) int {
	if len(retrySeconds) == 0 {
		return 10
	}
	if attempts <= 0 {
		attempts = 1
	}
	idx := attempts - 1
	if idx >= len(retrySeconds) {
		return retrySeconds[len(retrySeconds)-1]
	}
	return retrySeconds[idx]
}
