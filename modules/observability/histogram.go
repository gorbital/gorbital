package observability

import (
	"math"
	"slices"
	"time"
)

// bucketBounds are the upper bounds of the latency histogram's buckets; a
// last bucket holds everything slower. They are part of the stored data:
// every row of observability_minutes has one count per bucket, so changing
// them needs a migration that converts or drops the stored rows.
//
// Between 1 ms and 10 s each bound is at most 1.5 times the one before it
// (1, 1.5, 2, 3, 4, 5 and 7.5 per decade), which bounds the error of
// [Stats.Quantile] (see there).
var bucketBounds = [...]time.Duration{
	250 * time.Microsecond, 500 * time.Microsecond,
	time.Millisecond, 1500 * time.Microsecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond, 7500 * time.Microsecond,
	10 * time.Millisecond, 15 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, 40 * time.Millisecond, 50 * time.Millisecond, 75 * time.Millisecond,
	100 * time.Millisecond, 150 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond, 500 * time.Millisecond, 750 * time.Millisecond,
	time.Second, 1500 * time.Millisecond, 2 * time.Second, 3 * time.Second, 4 * time.Second, 5 * time.Second, 7500 * time.Millisecond,
	10 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute,
}

// BucketCount is the number of latency histogram buckets: one per bound in
// [BucketBounds], and one for slower requests.
const BucketCount = len(bucketBounds) + 1

// BucketBounds returns the upper bounds of the latency histogram's buckets,
// fastest first. Bucket i counts requests that took at most bound i and
// longer than bound i-1; the last bucket counts requests slower than every
// bound.
func BucketBounds() []time.Duration { return slices.Clone(bucketBounds[:]) }

// bucketOf returns the bucket index of a request that took d.
func bucketOf(d time.Duration) int {
	i, _ := slices.BinarySearch(bucketBounds[:], d) // first bound >= d
	return i
}

// Stats are request counts and latencies over some set of requests: a
// route, an instance, a minute, or everything in a time range.
type Stats struct {
	Requests int64
	// ClientErrors are 4xx responses, ServerErrors 5xx responses; a
	// handler that panicked counts as a server error.
	ClientErrors int64
	ServerErrors int64
	// DurationSum is the total time spent, DurationMax the slowest request.
	DurationSum time.Duration
	DurationMax time.Duration
	// Buckets holds [BucketCount] request counts, by [BucketBounds]. Nil
	// when there are no requests.
	Buckets []int64
}

// ErrorRate returns the share of requests that were server errors, from 0
// to 1; 0 without requests.
func (s Stats) ErrorRate() float64 {
	if s.Requests <= 0 {
		return 0
	}
	return float64(s.ServerErrors) / float64(s.Requests)
}

// Mean returns the average request duration; 0 without requests.
func (s Stats) Mean() time.Duration {
	if s.Requests <= 0 {
		return 0
	}
	return s.DurationSum / time.Duration(s.Requests)
}

// Quantile estimates the duration q (0 < q ≤ 1) of requests took at most,
// such as 0.95 for the 95th percentile; 0 without requests.
//
// The estimate finds the bucket holding the q-th request and interpolates
// linearly inside it, assuming requests are spread evenly across the
// bucket; the last bucket is interpolated up to [Stats.DurationMax], and no
// estimate exceeds it. The exact value lies in the same bucket, so the
// error is at most the bucket's width: under 0.5 ms for requests faster
// than 1 ms, at most 50% of the exact value between 1 ms and 10 s, and at
// most 100% above 10 s. Real latency distributions change little inside a
// bucket, so errors are usually a few percent (ADR-0064 records measured
// errors).
func (s Stats) Quantile(q float64) time.Duration {
	var total int64
	for _, n := range s.Buckets {
		total += n
	}
	if total <= 0 || q <= 0 || math.IsNaN(q) {
		return 0
	}
	q = min(q, 1)
	rank := q * float64(total) // the rank-th request, 1-based and fractional
	var before int64
	for i, n := range s.Buckets {
		if n == 0 || float64(before+n) < rank {
			before += n
			continue
		}
		var lower, upper time.Duration
		if i > 0 {
			lower = bucketBounds[min(i, len(bucketBounds))-1]
		}
		if i < len(bucketBounds) {
			upper = bucketBounds[i]
		} else {
			upper = max(s.DurationMax, lower)
		}
		est := lower + time.Duration(float64(upper-lower)*(rank-float64(before))/float64(n))
		if s.DurationMax > 0 {
			est = min(est, s.DurationMax)
		}
		return est
	}
	return s.DurationMax
}

// add adds o's counts to s.
func (s *Stats) add(o Stats) {
	s.Requests += o.Requests
	s.ClientErrors += o.ClientErrors
	s.ServerErrors += o.ServerErrors
	s.DurationSum += o.DurationSum
	s.DurationMax = max(s.DurationMax, o.DurationMax)
	if len(o.Buckets) == 0 {
		return
	}
	if s.Buckets == nil {
		s.Buckets = make([]int64, BucketCount)
	}
	for i := range min(len(o.Buckets), BucketCount) {
		s.Buckets[i] += o.Buckets[i]
	}
}
