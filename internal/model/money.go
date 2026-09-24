package model

import "math"

const MicrosPerYuan int64 = 1_000_000

func YuanToMicros(yuan float64) int64 {
	if math.IsNaN(yuan) || math.IsInf(yuan, 0) {
		return 0
	}
	return int64(math.Round(yuan * float64(MicrosPerYuan)))
}

func MicrosToYuan(micros int64) float64 {
	return float64(micros) / float64(MicrosPerYuan)
}
