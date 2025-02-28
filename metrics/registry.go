package metrics

import "github.com/ava-labs/libevm/metrics"

// Registry is a metrics registry to be used by metrics in coreth.
// We use it especially to avoid conflicts with imports, direct or
// indirect, of libevm packages with global scope metrics variables
// using the default registry. Coreth should avoid using the default
// registry to avoid any conflict.
var Registry = metrics.NewRegistry()
