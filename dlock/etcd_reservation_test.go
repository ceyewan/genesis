package dlock

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEtcdLockerAcquisitionReservationSerializesSameKey(t *testing.T) {
	locker := &etcdLocker{}

	const callers = 64
	start := make(chan struct{})
	releaseWinner := make(chan struct{})
	results := make(chan bool, callers)

	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			<-start
			reserved := locker.reserveAcquisition("same-key")
			results <- reserved
			if reserved {
				<-releaseWinner
				locker.releaseAcquisition("same-key")
			}
		}()
	}

	close(start)
	winners := 0
	for range callers {
		if <-results {
			winners++
		}
	}
	require.Equal(t, 1, winners)

	close(releaseWinner)
	wg.Wait()
	require.True(t, locker.reserveAcquisition("same-key"), "released keys must be acquirable again")
	locker.releaseAcquisition("same-key")
}

func TestEtcdLockerAcquisitionReservationIsPerKey(t *testing.T) {
	locker := &etcdLocker{}

	require.True(t, locker.reserveAcquisition("one"))
	require.True(t, locker.reserveAcquisition("two"))
	require.False(t, locker.reserveAcquisition("one"))

	locker.releaseAcquisition("one")
	locker.releaseAcquisition("two")
}
