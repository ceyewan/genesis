package dlock

import "container/list"

// lostTombstoneLimit bounds diagnostic history retained after ownership loss.
// Callers that obtained Lost while holding the lock keep their own channel and
// are unaffected by eviction from this lookup history.
const lostTombstoneLimit = 1024

type tombstoneRecord[T any] struct {
	value   T
	element *list.Element
}

// boundedTombstones is used only while the containing Locker's state mutex is
// held. It deliberately has no internal mutex so state transitions remain
// linearized by the Locker mutex.
type boundedTombstones[T any] struct {
	entries map[string]*tombstoneRecord[T]
	order   list.List
}

func newBoundedTombstones[T any]() boundedTombstones[T] {
	return boundedTombstones[T]{entries: make(map[string]*tombstoneRecord[T])}
}

func (s *boundedTombstones[T]) Put(key string, value T) {
	if s.entries == nil {
		s.entries = make(map[string]*tombstoneRecord[T])
	}
	if record, ok := s.entries[key]; ok {
		record.value = value
		s.order.MoveToBack(record.element)
		return
	}

	element := s.order.PushBack(key)
	s.entries[key] = &tombstoneRecord[T]{value: value, element: element}
	if len(s.entries) <= lostTombstoneLimit {
		return
	}

	oldest := s.order.Front()
	oldestKey := oldest.Value.(string)
	delete(s.entries, oldestKey)
	s.order.Remove(oldest)
}

func (s *boundedTombstones[T]) Get(key string) (T, bool) {
	record, ok := s.entries[key]
	if !ok {
		var zero T
		return zero, false
	}
	return record.value, true
}

func (s *boundedTombstones[T]) Delete(key string) {
	record, ok := s.entries[key]
	if !ok {
		return
	}
	delete(s.entries, key)
	s.order.Remove(record.element)
}

func (s *boundedTombstones[T]) Clear() {
	clear(s.entries)
	s.order.Init()
}

func (s *boundedTombstones[T]) Len() int { return len(s.entries) }
