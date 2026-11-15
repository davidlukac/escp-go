// Package hostportprod is production code: a literal of the configuration type is not a test's business.
package hostportprod

import "hostporttests"

var _ = hostporttests.Config{SwitchPort: 29808}
