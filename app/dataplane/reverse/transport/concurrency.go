package transport

const maxAssetConcurrency = 100

func clampAssetConcurrency(value int) int {
	if value < 1 {
		return 1
	}
	if value > maxAssetConcurrency {
		return maxAssetConcurrency
	}
	return value
}
