/*
Copyright 2014 The Prometheus Authors
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

package topk

import (
	"bytes"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

// The contents of this file are copied heavily from github.com/prometheus/client_golang/prometheus/vec.go

// MustCurryWith implements the Vec interface.
func (r *topkCurry) MustCurryWith(labels prometheus.Labels) TopK {
	n, err := r.CurryWith(labels)
	if err != nil {
		panic(err)
	}
	return n
}

// CurryWith implements the Vec interface.
func (r *topkCurry) CurryWith(labels prometheus.Labels) (TopK, error) {
	var (
		newCurry []curriedLabelValue
		oldCurry = r.curry
		iCurry   int
	)
	for i, label := range r.root.variableLabels {
		val, ok := labels[label]
		if iCurry < len(oldCurry) && oldCurry[iCurry].index == i {
			if ok {
				return nil, fmt.Errorf("label name %q is already curried", label)
			}
			newCurry = append(newCurry, oldCurry[iCurry])
			iCurry++
		} else {
			if !ok {
				continue // Label stays unset
			}
			newCurry = append(newCurry, curriedLabelValue{i, val})
		}
	}
	if leftover := len(oldCurry) + len(labels) - len(newCurry); leftover > 0 {
		return nil, fmt.Errorf("%d unknown label(s) found during currying", leftover)
	}

	return &topkCurry{
		curry: newCurry,
		root:  r.root,
	}, nil
}

func (r *topkCurry) compositeWithLabels(labels prometheus.Labels) (string, error) {
	if err := validateLabels(labels, len(r.root.variableLabels)-len(r.curry)); err != nil {
		return "", err
	}

	var (
		keyBuf bytes.Buffer
		curry  = r.curry
		iCurry int
	)

	for i, label := range r.root.variableLabels {
		val, ok := labels[label]
		if iCurry < len(curry) && curry[iCurry].index == i {
			if ok {
				return "", fmt.Errorf("label name %q is already curried", label)
			}
			keyBuf.WriteString(curry[iCurry].value)
			iCurry++
		} else {
			if !ok {
				return "", fmt.Errorf("label name %q missing in label map", label)
			}
			keyBuf.WriteString(val)
		}
		keyBuf.WriteByte(model.SeparatorByte)
	}
	return keyBuf.String(), nil
}

func (r *topkCurry) compositeWithLabelValues(lvs ...string) (string, error) {
	if err := validateLabelValues(lvs, len(r.root.variableLabels)-len(r.curry)); err != nil {
		return "", err
	}

	var (
		keyBuf        bytes.Buffer
		curry         = r.curry
		iVals, iCurry int
	)
	for i := 0; i < len(r.root.variableLabels); i++ {
		if iCurry < len(curry) && curry[iCurry].index == i {
			keyBuf.WriteString(curry[iCurry].value)
			iCurry++
		} else {
			keyBuf.WriteString(lvs[iVals])
			iVals++
		}
		keyBuf.WriteByte(model.SeparatorByte)
	}

	return keyBuf.String(), nil
}

func validateLabels(labels prometheus.Labels, expectCount int) error {
	if expectCount > len(labels) {
		return fmt.Errorf("not enough labels (missing %d)", expectCount-len(labels))
	} else if expectCount < len(labels) {
		return fmt.Errorf("received %d unrecognized labels", len(labels)-expectCount)
	}
	return nil
}

func validateLabelValues(lvs []string, expectCount int) error {
	if expectCount > len(lvs) {
		return fmt.Errorf("not enough labels (missing %d)", expectCount-len(lvs))
	} else if expectCount < len(lvs) {
		return fmt.Errorf("received %d unrecognized labels", len(lvs)-expectCount)
	}
	return nil
}

// GetMetricWith implements the Vec interface.
func (r *topkCurry) GetMetricWith(labels prometheus.Labels) (TopKBucket, error) {
	composite, err := r.compositeWithLabels(labels)
	if err != nil {
		return nil, err
	}
	return &topkWithLabelValues{compositeLabel: composite, root: r.root}, nil
}

// With implements the Vec interface.
func (r *topkCurry) With(labels prometheus.Labels) TopKBucket {
	composite, err := r.compositeWithLabels(labels)
	if err != nil {
		panic(err)
	}
	return &topkWithLabelValues{compositeLabel: composite, root: r.root}
}

// GetMetricWithLabelValues implements the Vec interface.
func (r *topkCurry) GetMetricWithLabelValues(lvs ...string) (TopKBucket, error) {
	composite, err := r.compositeWithLabelValues(lvs...)
	if err != nil {
		return nil, err
	}
	return &topkWithLabelValues{compositeLabel: composite, root: r.root}, nil
}

// WithLabelValues implements the Vec interface.
func (r *topkCurry) WithLabelValues(lvs ...string) TopKBucket {
	composite, err := r.compositeWithLabelValues(lvs...)
	if err != nil {
		panic(err)
	}
	return &topkWithLabelValues{compositeLabel: composite, root: r.root}
}
