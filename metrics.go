//+build metrics
package autodelete

import (
	"fmt"
	"strconv"

	"github.com/bwmarrin/discordgo"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/golang/protobuf/proto"
	dto "github.com/prometheus/client_model/go"
)

const (
	namespaceAutodelete = "autodelete"
)

const fieldErrorCode = "error_code"

// Report a Discord error to the error_code field of a CounterVec.
func reportDiscordError(metric *prometheus.CounterVec, err error) {
	if err == nil {
		return
	}
	if rErr, ok := err.(*discordgo.RESTError); ok {
		if rErr.Message == nil {
			metric.With(prometheus.Labels{"error_code": "missing"}).Increment()
		} else {
			metric.With(prometheus.Labels{"error_code": strconv.Itoa(rErr.Message.Code)}).Increment()
		}
	} else if false {
		// todo: add more discriminants
	} else {
		errType := fmt.Sprintf("%T", err)
		metric.With(prometheus.Labels{"error_code": errType}).Increment()
	}
}

var (
	descLiveMessagesDistribution = prometheus.NewDesc(
		namespaceAutodelete+"_live_messages_distr",
		"Histogram of live messages across all channels.",
		nil, nil,
	)
	boundsLiveMessages = []float64{
		0, 1, 2, 3, 5, 8, 13, 20, 30, 50, 80, 130, 200, 300, 500,
	}
	descKeepMessagesDistribution = prometheus.NewDesc(
		namespaceAutodelete+"_keep_messages_distr",
		"Histogram of kept messages across all channels.",
		nil, nil,
	)
	boundsKeepMessages = prometheus.LinearBuckets(0, 1, 12)
)

type channelCollector struct {
	bot *Bot
}

type temporaryHistogram struct {
	count       uint64
	sum         float64
	upperBounds []float64
	buckets     []uint64
}

// temporaryHistogram code:
// Copyright 2015 The Prometheus Authors
// SPDX-License-Identifier: Apache-2.0
// https://github.com/prometheus/client_golang/blob/master/prometheus/histogram.go#L263-L294
func (h *temporaryHistogram) Observe(v float64) {
	i := sort.SearchFloat64(h.upperBounds, v)
	h.sum += v
	if i < len(h.upperBounds) {
		h.buckets[i]++
	}
	h.count++
}

func (h *temporaryHistogram) Write(out *dto.Metric) error {
	his := &dto.Histogram{
		Bucket:      make([]*dto.Bucket, len(h.upperBounds)),
		SampleCount: proto.Uint64(h.count),
		SampleSum:   proto.Float64(h.sum),
	}
}


func (c *channelCollector) Describe(out chan<- *prometheus.Desc) {
	out <- descLiveMessagesDistribution
	out <- descKeepMessagesDistribution
}

func (c *channelCollector) Collect(chan<- Metric) {
	var liveH, keepH temporaryHistogram

}
