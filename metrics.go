package autodelete

import "github.com/prometheus/client_golang/prometheus"

// Prometheus duration bucket definitions.
var (
	// Durations that might be message live durations.
	//
	// Needs to specially include 864000 seconds == 240 hours
	bucketsDeletionTimes = []float64{
		0.0005, 0.003, 0.03, .1, .3, 1, 3, 10, 30, 100, 300, 1000, 3000, 10000, 30000, 300000, 1000000, 3000000, /* infinity bucket */
	}

	bucketsMessageCounts = []float64{
		1, 2, 3, 5, 8, 10, 20, 30, 50, 80, 100,
	}

	bucketsNetwork = prometheus.DefBuckets

	bucketsDiscordAPI = []float64{
		0.0005, 0.05, 0.1, 0.25, 0.5, 1, 2, 3, 5, 10, 25, 50,
	}
)

const nsAutodelete = "autodelete"
