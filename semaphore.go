package main

type semaphore chan interface{}

func newSemaphore(size int) semaphore {
	return make(semaphore, size)
}

func (s semaphore) acquire() {
	s <- struct{}{}
}

func (s semaphore) release() {
	<-s
}

func (s semaphore) wait() {
	for i := 0; i < cap(s); i++ {
		s.acquire()
	}
}
