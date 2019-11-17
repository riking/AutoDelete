/*
Copyright 2019 Kane York
Copyright 2019 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package topk provides a Metric/Collector implementation of a top-K streaming
// summary algorithm for use with high cardinality data.
//
// The github.com/dgryski/go-topk package is used to implement the calculations.
package topk

import (
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
)

// TopK is a metric package for estimating the top keys of a high-cardinality set.
//
// On every collection, the top "K" label pairs (where K is the value of
// opts.Buckets) are exported as Counters with variable labels, plus a parallel
// set of Gauges for the error bars.
//
// Usage: call one of the With() methods to receive a TopKBucket, and call the
// Observe method to record an observation. If any NaN values are passed to
// Observe, they are treated as 0 so as to not pollute the storage.
type TopK interface {
	prometheus.Collector

	CurryWith(prometheus.Labels) (TopK, error)
	MustCurryWith(prometheus.Labels) TopK
	GetMetricWith(prometheus.Labels) (TopKBucket, error)
	GetMetricWithLabelValues(lvs ...string) (TopKBucket, error)
	With(prometheus.Labels) TopKBucket
	WithLabelValues(lvs ...string) TopKBucket
}

type TopKBucket interface {
	Observe(float64)
	Inc()
}

type TopKOpts struct {
	// Namespace, Subsystem, and Name are components of the fully-qualified
	// name of the TopK (created by joining these components with "_").
	// Only Name is mandatory, the others merely help structuring the name.
	// Note that the fully-qualified name of the TopK must be a valid
	// Prometheus metric name.
	Namespace string
	Subsystem string
	Name      string

	// Help provides information about this Histogram.
	//
	// Metrics with the same fully-qualified name must have the same Help
	// string.
	Help string

	// ConstLabels are used to attach fixed labels to this metric. Metrics
	// with the same fully-qualified name must have the same label names in
	// their ConstLabels.
	//
	// ConstLabels are only used rarely. In particular, do not use them to
	// attach the same labels to all your metrics. Those use cases are
	// better covered by target labels set by the scraping Prometheus
	// server, or by one specific metric (e.g. a build_info or a
	// machine_role metric). See also
	// https://prometheus.io/docs/instrumenting/writing_exporters/#target-labels,-not-static-scraped-labels
	ConstLabels prometheus.Labels

	// Buckets provides the number of metric streams that this metric is
	// expected to keep an accurate count for (the "K" in top-K).
	Buckets uint64

	// To help preserve privacy, any values under the ReportingThreshold
	// are not collected.
	ReportingThreshold float64
}

type topkRoot struct {
	// unfortunately, all access to the Stream needs to be protected
	streamMtx sync.Mutex
	stream    *Stream

	countDesc *prometheus.Desc
	errDesc   *prometheus.Desc

	variableLabels  []string
	reportThreshold float64
}

type curriedLabelValue struct {
	index int
	value string
}

type topkCurry struct {
	curry []curriedLabelValue
	root  *topkRoot
}

type topkWithLabelValues struct {
	compositeLabel string
	root           *topkRoot
}

type resolvedMetric struct {
	value      float64
	labelPairs []*dto.LabelPair
	ts         int64
}

var (
	_ TopK                = &topkCurry{}
	_ TopKBucket          = &topkWithLabelValues{}
	_ prometheus.Observer = &topkWithLabelValues{}
)

// NewTopK constructs a new TopK metric container.
func NewTopK(opts TopKOpts, labelNames []string) TopK {
	fqName := prometheus.BuildFQName(opts.Namespace, opts.Subsystem, opts.Name)

	// Take a copy to avoid mutation
	varLabels := append([]string(nil), labelNames...)

	root := &topkRoot{
		stream: NewStream(int(opts.Buckets)),

		countDesc: prometheus.NewDesc(
			fqName, opts.Help, varLabels, opts.ConstLabels),
		errDesc: prometheus.NewDesc(
			fmt.Sprintf("%s_error", fqName), opts.Help, varLabels, opts.ConstLabels),

		variableLabels:  varLabels,
		reportThreshold: opts.ReportingThreshold,
	}
	return &topkCurry{root: root, curry: nil}
}

func (r *topkCurry) Describe(ch chan<- *prometheus.Desc) {
	ch <- r.root.countDesc
	ch <- r.root.errDesc
}

var labelParseSplit = string([]byte{model.SeparatorByte})

func (r *topkCurry) Collect(ch chan<- prometheus.Metric) {
	r.root.streamMtx.Lock()
	elts := r.root.stream.Keys()
	r.root.streamMtx.Unlock()

	zeroSent := false

	for _, e := range elts {
		if e.Count < r.root.reportThreshold {
			// Do not collect metrics under the reporting threshold
			continue
		}
		split := strings.Split(e.Key, labelParseSplit)
		if len(split) != len(r.root.variableLabels)+1 {
			panic("bad label-string value in topk")
		}
		lvs := split[:len(r.root.variableLabels)]
		ch <- prometheus.MustNewConstMetric(r.root.countDesc, prometheus.CounterValue, e.Count, lvs...)
		if e.Error != 0 || !zeroSent {
			ch <- prometheus.MustNewConstMetric(r.root.errDesc, prometheus.GaugeValue, -e.Error, lvs...)
			if e.Error == 0 {
				zeroSent = true
			}
		}
	}
}

func (b *topkWithLabelValues) Observe(v float64) {
	b.root.streamMtx.Lock()
	defer b.root.streamMtx.Unlock()
	b.root.stream.Insert(b.compositeLabel, v)
}

func (b *topkWithLabelValues) Inc() {
	b.Observe(1)
}
