package common

import (
	"sync/atomic"
	"time"
)

var (
	ntpEpoch = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
)

type atomicBool int32
type NtpTime uint64

func (a *atomicBool) set(value bool) (swapped bool) {
	if value {
		return atomic.SwapInt32((*int32)(a), 1) == 0
	}
	return atomic.SwapInt32((*int32)(a), 0) == 1
}

func (a *atomicBool) get() bool {
	return atomic.LoadInt32((*int32)(a)) != 0
}

func ntpToMillisSinceEpoch(ntp uint64) uint64 {
	// ntp time since epoch calculate fractional ntp as milliseconds
	// (lower 32 bits stored as 1/2^32 seconds) and add
	// ntp seconds (stored in higher 32 bits) as milliseconds
	return (((ntp & 0xFFFFFFFF) * 1000) >> 32) + ((ntp >> 32) * 1000)
}

func (t NtpTime) Duration() time.Duration {
	sec := (t >> 32) * 1e9
	frac := (t & 0xffffffff) * 1e9
	nsec := frac >> 32
	if uint32(frac) >= 0x80000000 {
		nsec++
	}
	return time.Duration(sec + nsec)
}

func (t NtpTime) Time() time.Time {
	return ntpEpoch.Add(t.Duration())
}

func toNtpTime(t time.Time) NtpTime {
	nsec := uint64(t.Sub(ntpEpoch))
	sec := nsec / 1e9
	nsec = (nsec - sec*1e9) << 32
	frac := nsec / 1e9
	if nsec%1e9 >= 1e9/2 {
		frac++
	}
	return NtpTime(sec<<32 | frac)
}
