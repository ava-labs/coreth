package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type NoopRegister struct{}

func (n *NoopRegister) Register(prometheus.Collector) error  { return nil }
func (n *NoopRegister) MustRegister(...prometheus.Collector) {}
func (n *NoopRegister) Unregister(prometheus.Collector) bool { return true }
func (n *NoopRegister) Gather() ([]*dto.MetricFamily, error) { return nil, nil }
