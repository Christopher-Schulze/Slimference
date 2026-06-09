package compression

import (
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// CompressiblePrefixEnd returns the exclusive end index of the prefix that Layer 1 may
// transform. Messages with index >= end are inside the sliding window: the last
// windowExchanges user turns (each message with role "user" starts one exchange).
//
// If there are no user messages, the entire slice may be compressed (unusual API usage).
// If the number of user turns is <= windowExchanges, nothing is compressible (returns 0).
func CompressiblePrefixEnd(messages []types.Message, windowExchanges int) int {
	if windowExchanges < 1 {
		windowExchanges = 1
	}
	var userIdx []int
	for i, m := range messages {
		if m.Role == "user" {
			userIdx = append(userIdx, i)
		}
	}
	if len(userIdx) == 0 {
		return len(messages)
	}
	if len(userIdx) <= windowExchanges {
		return 0
	}
	return userIdx[len(userIdx)-windowExchanges]
}
